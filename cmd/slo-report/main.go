// Command slo-report turns SLO definitions into Prometheus / VictoriaMetrics
// burn rate alerts and produces error budget reports for SLO reviews.
//
//	slo-report validate slos.yaml
//	slo-report generate slos.yaml > rules.yaml
//	slo-report report --prometheus http://prometheus:9090 slos.yaml
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/OTA-EITA/slo-report/internal/burn"
	"github.com/OTA-EITA/slo-report/internal/prom"
	"github.com/OTA-EITA/slo-report/internal/report"
	"github.com/OTA-EITA/slo-report/internal/rules"
	"github.com/OTA-EITA/slo-report/internal/spec"
)

const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

const usage = `usage: slo-report <command> [flags] SLO_FILE

commands:
  validate   check an SLO file
  generate   write Prometheus / vmalert recording and burn rate alert rules
  report     query Prometheus and print the error budget of every SLO
  version    print the version
`

func run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "validate":
		return cmdValidate(args[1:], stdout, stderr)
	case "generate":
		return cmdGenerate(args[1:], stdout, stderr)
	case "report":
		return cmdReport(args[1:], stdout, stderr, now)
	case "version", "--version":
		fmt.Fprintln(stdout, "slo-report", version)
		return exitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

func loadFile(fs *flag.FlagSet, stderr io.Writer) (spec.File, bool) {
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "error: exactly one SLO file is required")
		return spec.File{}, false
	}
	data, err := os.ReadFile(fs.Arg(0)) // #nosec G304 -- the SLO file path is the tool's input
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return spec.File{}, false
	}
	f, err := spec.Load(data)
	if err != nil {
		fmt.Fprintf(stderr, "error: %s: %v\n", fs.Arg(0), err)
		return spec.File{}, false
	}
	return f, true
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("slo-report "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func parse(fs *flag.FlagSet, args []string) (int, bool) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK, false
		}
		return exitUsage, false
	}
	return 0, true
}

func cmdValidate(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("validate", stderr)
	if code, ok := parse(fs, args); !ok {
		return code
	}
	f, ok := loadFile(fs, stderr)
	if !ok {
		return exitFail
	}
	for _, s := range f.SLOs {
		fmt.Fprintf(stdout, "ok  %-30s objective=%v%% window=%s budget=%.4g%%\n", s.Name, s.Objective, s.Window, 100*s.ErrorBudget())
	}
	return exitOK
}

func cmdGenerate(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("generate", stderr)
	out := fs.String("o", "", "write the rule file here instead of stdout")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	f, ok := loadFile(fs, stderr)
	if !ok {
		return exitFail
	}
	data, err := rules.Generate(f, burn.DefaultPolicies)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitFail
	}
	if *out == "" {
		_, err = stdout.Write(data)
	} else {
		err = os.WriteFile(*out, data, 0o600)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitFail
	}
	return exitOK
}

func cmdReport(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := newFlagSet("report", stderr)
	url := fs.String("prometheus", os.Getenv("PROMETHEUS_URL"), "Prometheus-compatible API base URL (or $PROMETHEUS_URL)")
	tokenFile := fs.String("bearer-token-file", "", "file containing a bearer token for the API")
	output := fs.String("output", "table", "output format: table, json or markdown")
	failOn := fs.String("fail-on", "exhausted", "exit 1 when any SLO is: exhausted, at-risk or never")
	timeout := fs.Duration("timeout", time.Minute, "overall timeout")
	if code, ok := parse(fs, args); !ok {
		return code
	}
	if *url == "" {
		fmt.Fprintln(stderr, "error: --prometheus is required")
		return exitUsage
	}
	format, err := report.ParseFormat(*output)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitUsage
	}
	if *failOn != "exhausted" && *failOn != "at-risk" && *failOn != "never" {
		fmt.Fprintf(stderr, "error: --fail-on must be exhausted, at-risk or never, got %q\n", *failOn)
		return exitUsage
	}
	f, ok := loadFile(fs, stderr)
	if !ok {
		return exitUsage
	}
	client := prom.Client{BaseURL: *url}
	if *tokenFile != "" {
		tok, err := os.ReadFile(*tokenFile) // #nosec G304 -- path given by the operator
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return exitUsage
		}
		client.BearerToken = strings.TrimSpace(string(tok))
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	at := now()
	rows, err := report.Evaluate(ctx, client, f, at)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitFail
	}
	if err := report.Write(stdout, rows, format, at); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitFail
	}
	for _, r := range rows {
		if r.Verdict == "EXHAUSTED" && *failOn != "never" {
			return exitFail
		}
		if r.Verdict == "AT RISK" && *failOn == "at-risk" {
			return exitFail
		}
	}
	return exitOK
}

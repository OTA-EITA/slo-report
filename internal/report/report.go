// Package report evaluates SLOs against a Prometheus-compatible API and
// renders an error budget report.
package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/OTA-EITA/slo-report/internal/burn"
	"github.com/OTA-EITA/slo-report/internal/prom"
	"github.com/OTA-EITA/slo-report/internal/spec"
)

// BurnWindows are the burn rates shown in reports.
var BurnWindows = []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour}

// Querier evaluates instant queries; *prom.Client implements it.
type Querier interface {
	Scalar(ctx context.Context, query string, at time.Time) (float64, error)
}

// Row is the evaluated state of one SLO.
type Row struct {
	Name      string             `json:"name"`
	Service   string             `json:"service"`
	Objective float64            `json:"objective"`
	Window    string             `json:"window"`
	NoData    bool               `json:"noData,omitempty"`
	Status    burn.Status        `json:"-"`
	SLI       float64            `json:"sli"`
	Remaining float64            `json:"budgetRemaining"`
	Burn      map[string]float64 `json:"burnRates"`
	TTE       string             `json:"timeToExhaustion,omitempty"`
	Verdict   string             `json:"verdict"`
}

// Evaluate queries every SLO at time at.
func Evaluate(ctx context.Context, q Querier, f spec.File, at time.Time) ([]Row, error) {
	var rows []Row
	for _, s := range f.SLOs {
		row := Row{Name: s.Name, Service: s.Service, Objective: s.Objective, Window: s.Window.String(), Burn: map[string]float64{}}
		windowRatio, err := q.Scalar(ctx, s.ErrorRatioQuery(s.Window), at)
		if errors.Is(err, prom.ErrNoData) {
			row.NoData, row.Verdict = true, "NO DATA"
			rows = append(rows, row)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		ratios := map[time.Duration]float64{}
		for _, w := range BurnWindows {
			r, err := q.Scalar(ctx, s.ErrorRatioQuery(spec.Duration(w)), at)
			if errors.Is(err, prom.ErrNoData) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("%s (%s): %w", s.Name, spec.Duration(w), err)
			}
			ratios[w] = r
		}
		st := burn.Compute(s.ErrorBudget(), time.Duration(s.Window), windowRatio, ratios)
		row.Status = st
		row.SLI = 100 * (1 - windowRatio)
		row.Remaining = 100 * st.BudgetRemaining
		for w, b := range st.BurnRates {
			row.Burn[spec.Duration(w).String()] = b
		}
		if st.TimeToExhaustion > 0 {
			row.TTE = humanDuration(st.TimeToExhaustion)
		}
		row.Verdict = st.Verdict()
		rows = append(rows, row)
	}
	return rows, nil
}

func humanDuration(d time.Duration) string {
	if d >= 48*time.Hour {
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// Format is an output format.
type Format string

// Supported formats.
const (
	Table    Format = "table"
	JSON     Format = "json"
	Markdown Format = "markdown"
)

// ParseFormat validates a format name.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case Table, JSON, Markdown:
		return Format(s), nil
	}
	return "", fmt.Errorf("unknown output format %q (want table, json or markdown)", s)
}

// Write renders rows.
func Write(w io.Writer, rows []Row, f Format, at time.Time) error {
	switch f {
	case JSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			GeneratedAt time.Time `json:"generatedAt"`
			SLOs        []Row     `json:"slos"`
		}{at.UTC(), rows})
	case Markdown:
		return writeMarkdown(w, rows, at)
	default:
		return writeTable(w, rows)
	}
}

func cells(r Row) []string {
	if r.NoData {
		return []string{r.Name, r.Service, pct(r.Objective), r.Window, "-", "-", "-", "-", "-", "-", r.Verdict}
	}
	out := []string{r.Name, r.Service, pct(r.Objective), r.Window, fmt.Sprintf("%.3f%%", r.SLI), fmt.Sprintf("%.1f%%", r.Remaining)}
	for _, w := range BurnWindows {
		key := spec.Duration(w).String()
		if b, ok := r.Burn[key]; ok {
			out = append(out, fmt.Sprintf("%.2fx", b))
		} else {
			out = append(out, "-")
		}
	}
	tte := r.TTE
	if tte == "" {
		tte = "-"
	}
	return append(out, tte, r.Verdict)
}

var header = []string{"SLO", "SERVICE", "OBJECTIVE", "WINDOW", "SLI", "BUDGET LEFT", "BURN 1H", "BURN 6H", "BURN 1D", "EXHAUSTED IN", "VERDICT"}

func pct(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", f), "0"), ".") + "%"
}

func writeTable(w io.Writer, rows []Row) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(cells(r), "\t"))
	}
	return tw.Flush()
}

func writeMarkdown(w io.Writer, rows []Row, at time.Time) error {
	var b strings.Builder
	fmt.Fprintf(&b, "## SLO report (%s)\n\n", at.UTC().Format("2006-01-02 15:04 UTC"))
	b.WriteString("| " + strings.Join(header, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat("---|", len(header)) + "\n")
	for _, r := range rows {
		c := cells(r)
		c[len(c)-1] = badge(r.Verdict) + " " + r.Verdict
		b.WriteString("| " + strings.Join(c, " | ") + " |\n")
	}
	b.WriteString("\nBurn rate 1.00x spends exactly the error budget over the SLO window. \"Exhausted in\" assumes the 6h burn rate continues.\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func badge(v string) string {
	switch v {
	case "EXHAUSTED":
		return "🔴"
	case "AT RISK":
		return "🟡"
	case "HEALTHY":
		return "🟢"
	default:
		return "⚪"
	}
}

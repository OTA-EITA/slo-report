package rules

import (
	"os"
	"strings"
	"testing"

	"github.com/OTA-EITA/slo-report/internal/burn"
	"github.com/OTA-EITA/slo-report/internal/spec"
)

func loadExample(t *testing.T) spec.File {
	t.Helper()
	data, err := os.ReadFile("../../examples/slos.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := spec.Load(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGenerateMatchesGoldenFile(t *testing.T) {
	got, err := Generate(loadExample(t), burn.DefaultPolicies)
	if err != nil {
		t.Fatal(err)
	}
	const golden = "../../examples/rules.yaml"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("generated rules differ from %s; run UPDATE_GOLDEN=1 go test ./internal/rules", golden)
	}
}

func TestGenerateContents(t *testing.T) {
	out, err := Generate(loadExample(t), burn.DefaultPolicies)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"record: slo:error_ratio:rate5m",
		"record: slo:error_ratio:rate3d",
		`slo:error_ratio:rate1h{slo="api-availability"} > 0.0144`,
		`slo:error_ratio:rate3d{slo="api-availability"} > 0.001`,
		`slo:error_ratio:rate1h{slo="api-latency"} > 0.144`,
		"runbook_url: https://example.com/runbooks/api-availability",
		"team: platform",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rules are missing %q", want)
		}
	}
	names, err := Names(out)
	if err != nil {
		t.Fatal(err)
	}
	// 7 recording rules and 4 alerts per SLO.
	if len(names) != 22 {
		t.Fatalf("got %d rules: %v", len(names), names)
	}
}

func TestShortSLOWindowDropsLongPolicies(t *testing.T) {
	f := loadExample(t)
	d, _ := spec.ParseDuration("1d")
	f.SLOs = f.SLOs[:1]
	f.SLOs[0].Window = d
	out, err := Generate(f, burn.DefaultPolicies)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "rate3d") || strings.Contains(string(out), "long_window: 1d") {
		t.Fatal("policies whose long window is not shorter than the SLO window must be skipped")
	}
}

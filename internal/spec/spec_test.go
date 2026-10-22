package spec

import (
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{"5m": 5 * time.Minute, "6h": 6 * time.Hour, "30d": 30 * 24 * time.Hour, "4w": 28 * 24 * time.Hour}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil || time.Duration(got) != want {
			t.Errorf("ParseDuration(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "0d", "1.5h", "30s", "d", "1y"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should fail", bad)
		}
	}
}

func TestDurationString(t *testing.T) {
	for in, want := range map[string]string{"60m": "1h", "24h": "1d", "14d": "2w", "90m": "90m", "30d": "30d"} {
		d, _ := ParseDuration(in)
		if d.String() != want {
			t.Errorf("%s -> %s, want %s", in, d.String(), want)
		}
	}
}

const valid = `
slos:
  - name: api-availability
    service: api
    objective: 99.9
    window: 30d
    sli:
      errors: sum(rate(http_requests_total{job="api",code=~"5.."}[${window}]))
      total: sum(rate(http_requests_total{job="api"}[${window}]))
`

func TestLoadValid(t *testing.T) {
	f, err := Load([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	s := f.SLOs[0]
	if b := s.ErrorBudget(); b < 0.000999 || b > 0.001001 {
		t.Errorf("budget = %v", b)
	}
	q := s.ErrorRatioQuery(Duration(time.Hour))
	if !strings.Contains(q, `code=~"5.."}[1h]`) || strings.Contains(q, WindowPlaceholder) {
		t.Errorf("query = %s", q)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	if _, err := Load([]byte(strings.Replace(valid, "objective", "objectve", 1))); err == nil {
		t.Fatal("typos must be rejected")
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	bad := `
slos:
  - name: Bad_Name
    objective: 100
    window: 30d
    sli: {errors: "x", total: "y[${window}]"}
  - name: dup
    service: s
    objective: 99
    window: 7d
    sli: {errors: "a[${window}]", total: "b[${window}]"}
  - name: dup
    service: s
    objective: 99
    window: 7d
    sli: {errors: "a[${window}]", total: "b[${window}]"}
`
	_, err := Load([]byte(bad))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"name must be lowercase", "service is required", "objective must be between", "sli.errors must contain", "duplicate name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if _, err := Load([]byte("slos: []")); err == nil {
		t.Error("an empty file must be rejected")
	}
}

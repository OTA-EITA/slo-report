package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/OTA-EITA/slo-report/internal/prom"
	"github.com/OTA-EITA/slo-report/internal/spec"
)

// fakeQuerier answers by matching the range selector in the query.
type fakeQuerier map[string]any

func (f fakeQuerier) Scalar(_ context.Context, query string, _ time.Time) (float64, error) {
	for window, v := range f {
		if strings.Contains(query, "["+window+"]") {
			switch v := v.(type) {
			case float64:
				return v, nil
			case error:
				return 0, v
			}
		}
	}
	return 0, prom.ErrNoData
}

func file(t *testing.T) spec.File {
	t.Helper()
	f, err := spec.Load([]byte(`
slos:
  - name: api-availability
    service: api
    objective: 99.9
    window: 30d
    sli:
      errors: sum(rate(errors_total[${window}]))
      total: sum(rate(requests_total[${window}]))
`))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

var at = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)

func TestEvaluate(t *testing.T) {
	rows, err := Evaluate(context.Background(), fakeQuerier{"30d": 0.0004, "1h": 0.002, "6h": 0.001, "1d": 0.0005}, file(t), at)
	if err != nil {
		t.Fatal(err)
	}
	r := rows[0]
	if r.Verdict != "HEALTHY" || r.Remaining < 59.99 || r.Remaining > 60.01 || r.Burn["1h"] != 2 || r.TTE != "18.0d" {
		t.Fatalf("got %+v", r)
	}
	if r.SLI < 99.9599 || r.SLI > 99.9601 {
		t.Fatalf("SLI %v", r.SLI)
	}
}

func TestEvaluateNoData(t *testing.T) {
	rows, err := Evaluate(context.Background(), fakeQuerier{}, file(t), at)
	if err != nil || !rows[0].NoData || rows[0].Verdict != "NO DATA" {
		t.Fatalf("got %+v, %v", rows, err)
	}
}

func TestEvaluateErrors(t *testing.T) {
	if _, err := Evaluate(context.Background(), fakeQuerier{"30d": errors.New("boom")}, file(t), at); err == nil {
		t.Fatal("query errors must be returned")
	}
	if _, err := Evaluate(context.Background(), fakeQuerier{"30d": 0.0001, "1h": errors.New("boom")}, file(t), at); err == nil {
		t.Fatal("burn window errors must be returned")
	}
}

func TestWriteFormats(t *testing.T) {
	rows, _ := Evaluate(context.Background(), fakeQuerier{"30d": 0.002, "1h": 0.02, "6h": 0.01}, file(t), at)
	var b bytes.Buffer
	if err := Write(&b, rows, Table, at); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BUDGET LEFT", "99.9%", "99.800%", "-100.0%", "20.00x", "EXHAUSTED"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("table missing %q:\n%s", want, b.String())
		}
	}
	b.Reset()
	if err := Write(&b, rows, Markdown, at); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "## SLO report (2026-10-22 00:00 UTC)") || !strings.Contains(b.String(), "🔴 EXHAUSTED") {
		t.Errorf("markdown:\n%s", b.String())
	}
	b.Reset()
	if err := Write(&b, rows, JSON, at); err != nil {
		t.Fatal(err)
	}
	var doc struct{ SLOs []Row }
	if err := json.Unmarshal(b.Bytes(), &doc); err != nil || doc.SLOs[0].Verdict != "EXHAUSTED" {
		t.Fatalf("json: %v %s", err, b.String())
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Error("unknown formats must be rejected")
	}
}

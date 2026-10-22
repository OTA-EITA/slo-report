package burn

import (
	"testing"
	"time"
)

const day = 24 * time.Hour

func TestFactorsMatchTheWorkbook(t *testing.T) {
	want := []float64{14.4, 6, 3, 1}
	for i, p := range DefaultPolicies {
		if got := p.Factor(30 * day); got != want[i] {
			t.Errorf("policy %d: factor %v, want %v", i, got, want[i])
		}
	}
	// A 7 day SLO burns its budget faster, so the same consumption needs a lower rate.
	if got := DefaultPolicies[0].Factor(7 * day); got != 3.36 {
		t.Errorf("7d page factor = %v, want 3.36", got)
	}
}

func TestWindows(t *testing.T) {
	got := Windows(DefaultPolicies, 30*day)
	want := []time.Duration{5 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 6 * time.Hour, day, 3 * day}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	// A 1 day SLO cannot use the 1d and 3d policies.
	if got := Windows(DefaultPolicies, day); len(got) != 4 {
		t.Fatalf("1d windows = %v", got)
	}
}

func TestCompute(t *testing.T) {
	s := Compute(0.001, 30*day, 0.0004, map[time.Duration]float64{time.Hour: 0.002, 6 * time.Hour: 0.001})
	if s.BudgetRemaining != 0.6 || s.Verdict() != "HEALTHY" {
		t.Fatalf("got %+v", s)
	}
	if s.BurnRates[time.Hour] != 2 || s.BurnRates[6*time.Hour] != 1 {
		t.Fatalf("burn rates %v", s.BurnRates)
	}
	// 60% of a 30 day budget at burn rate 1 lasts 18 days.
	if s.TimeToExhaustion != 18*day {
		t.Fatalf("time to exhaustion %v", s.TimeToExhaustion)
	}
	if v := Compute(0.001, 30*day, 0.0009, nil).Verdict(); v != "AT RISK" {
		t.Errorf("verdict %s", v)
	}
	if v := Compute(0.001, 30*day, 0.002, nil); v.Verdict() != "EXHAUSTED" || v.TimeToExhaustion != 0 {
		t.Errorf("got %+v", v)
	}
}

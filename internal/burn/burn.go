// Package burn implements the multi-window, multi-burn-rate alerting policy
// from the Google SRE Workbook ("Alerting on SLOs", chapter 5) and the error
// budget arithmetic used by reports.
package burn

import (
	"math"
	"time"
)

// Policy is one alerting condition: alert when the error budget burns fast
// enough that Consumption of the whole budget would be spent within Long,
// confirmed over the Short window so the alert resets quickly after recovery.
type Policy struct {
	Severity    string
	Long        time.Duration
	Short       time.Duration
	Consumption float64
	For         time.Duration
}

// DefaultPolicies are the workbook's recommended thresholds: page on 2% of the
// budget in 1h or 5% in 6h, open a ticket on 10% in 1d or 10% in 3d.
var DefaultPolicies = []Policy{
	{Severity: "page", Long: time.Hour, Short: 5 * time.Minute, Consumption: 0.02, For: 2 * time.Minute},
	{Severity: "page", Long: 6 * time.Hour, Short: 30 * time.Minute, Consumption: 0.05, For: 15 * time.Minute},
	{Severity: "ticket", Long: 24 * time.Hour, Short: 2 * time.Hour, Consumption: 0.10, For: time.Hour},
	{Severity: "ticket", Long: 72 * time.Hour, Short: 6 * time.Hour, Consumption: 0.10, For: 3 * time.Hour},
}

// Factor is the burn rate that triggers p for an SLO window. For a 30 day
// window it yields the workbook's 14.4, 6, 3 and 1.
func (p Policy) Factor(window time.Duration) float64 {
	return round(p.Consumption * float64(window) / float64(p.Long))
}

// Applies reports whether the policy makes sense for the SLO window: the long
// window must be shorter than the SLO window itself.
func (p Policy) Applies(window time.Duration) bool { return p.Long < window }

// Windows returns every distinct evaluation window the policies need.
func Windows(ps []Policy, window time.Duration) []time.Duration {
	seen := map[time.Duration]bool{}
	var out []time.Duration
	for _, p := range ps {
		if !p.Applies(window) {
			continue
		}
		for _, w := range []time.Duration{p.Short, p.Long} {
			if !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	sortDurations(out)
	return out
}

func sortDurations(ds []time.Duration) {
	for i := 1; i < len(ds); i++ {
		for j := i; j > 0 && ds[j] < ds[j-1]; j-- {
			ds[j], ds[j-1] = ds[j-1], ds[j]
		}
	}
}

// round keeps thresholds readable (14.4 instead of 14.399999999999999).
func round(f float64) float64 { return math.Round(f*1e6) / 1e6 }

// Status summarises an SLO for a report.
type Status struct {
	// ErrorRatio over the whole SLO window.
	ErrorRatio float64
	// BudgetRemaining is the fraction of the error budget left; negative when exhausted.
	BudgetRemaining float64
	// BurnRates per window: 1 means spending exactly the budget over the SLO window.
	BurnRates map[time.Duration]float64
	// TimeToExhaustion at the current 6h burn rate; zero when not burning.
	TimeToExhaustion time.Duration
}

// Compute derives the report status from measured error ratios.
func Compute(budget float64, window time.Duration, windowRatio float64, ratios map[time.Duration]float64) Status {
	s := Status{ErrorRatio: windowRatio, BurnRates: map[time.Duration]float64{}}
	if budget <= 0 {
		return s
	}
	s.BudgetRemaining = round(1 - windowRatio/budget)
	for w, r := range ratios {
		s.BurnRates[w] = round(r / budget)
	}
	if burn := s.BurnRates[6*time.Hour]; burn > 0 && s.BudgetRemaining > 0 {
		s.TimeToExhaustion = time.Duration(s.BudgetRemaining * float64(window) / burn).Round(time.Minute)
	}
	return s
}

// Verdict labels a status for humans.
func (s Status) Verdict() string {
	switch {
	case s.BudgetRemaining < 0:
		return "EXHAUSTED"
	case s.BudgetRemaining < 0.25:
		return "AT RISK"
	default:
		return "HEALTHY"
	}
}

// Package spec loads and validates SLO definitions.
package spec

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Duration is a Prometheus-style duration such as 5m, 6h or 30d.
type Duration time.Duration

var durationRE = regexp.MustCompile(`^(\d+)(m|h|d|w)$`)

// ParseDuration accepts minutes, hours, days and weeks.
func ParseDuration(s string) (Duration, error) {
	m := durationRE.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q (want e.g. 5m, 6h, 30d, 4w)", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n == 0 {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
	return Duration(time.Duration(n) * unit), nil
}

// String formats the duration in the largest unit that divides it exactly,
// which is what PromQL range selectors and rule names expect.
func (d Duration) String() string {
	td := time.Duration(d)
	switch {
	case td%(7*24*time.Hour) == 0:
		return fmt.Sprintf("%dw", td/(7*24*time.Hour))
	case td%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", td/(24*time.Hour))
	case td%time.Hour == 0:
		return fmt.Sprintf("%dh", td/time.Hour)
	default:
		return fmt.Sprintf("%dm", td/time.Minute)
	}
}

// UnmarshalYAML implements yaml.Unmarshaler for scalar durations.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	v, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

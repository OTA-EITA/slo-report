package spec

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// WindowPlaceholder is replaced with a range such as 5m in SLI queries.
const WindowPlaceholder = "${window}"

// File is the top level of an SLO definition file.
type File struct {
	SLOs []SLO `yaml:"slos"`
}

// SLO is a request-based service level objective.
type SLO struct {
	Name        string `yaml:"name"`
	Service     string `yaml:"service"`
	Description string `yaml:"description"`
	// Objective is the target percentage of good events, e.g. 99.9.
	Objective float64  `yaml:"objective"`
	Window    Duration `yaml:"window"`
	SLI       SLI      `yaml:"sli"`
	// RunbookURL is attached to alerts.
	RunbookURL string            `yaml:"runbookURL"`
	Labels     map[string]string `yaml:"labels"`
}

// SLI defines the error ratio as errors / total. Both queries must contain
// ${window}, which is replaced with each evaluation window.
type SLI struct {
	Errors string `yaml:"errors"`
	Total  string `yaml:"total"`
}

// ErrorBudget is the allowed error ratio, e.g. 0.001 for 99.9%.
func (s SLO) ErrorBudget() float64 { return 1 - s.Objective/100 }

// ErrorRatioQuery returns the PromQL error ratio over window.
func (s SLO) ErrorRatioQuery(window Duration) string {
	w := window.String()
	return "(" + strings.ReplaceAll(s.SLI.Errors, WindowPlaceholder, w) + ")\n/\n(" +
		strings.ReplaceAll(s.SLI.Total, WindowPlaceholder, w) + ")"
}

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Load parses and validates an SLO file. Unknown fields are rejected so that
// typos such as "objectve" do not silently fall back to zero.
func Load(data []byte) (File, error) {
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return f, fmt.Errorf("parse: %w", err)
	}
	return f, f.Validate()
}

// Validate checks every SLO and reports all problems at once.
func (f File) Validate() error {
	if len(f.SLOs) == 0 {
		return errors.New("no slos defined")
	}
	var errs []error
	seen := map[string]bool{}
	for i, s := range f.SLOs {
		where := fmt.Sprintf("slos[%d] (%s)", i, s.Name)
		if !nameRE.MatchString(s.Name) {
			errs = append(errs, fmt.Errorf("%s: name must be lowercase letters, digits and '-'", where))
		}
		if seen[s.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate name", where))
		}
		seen[s.Name] = true
		if s.Service == "" {
			errs = append(errs, fmt.Errorf("%s: service is required", where))
		}
		if s.Objective <= 0 || s.Objective >= 100 {
			errs = append(errs, fmt.Errorf("%s: objective must be between 0 and 100 (exclusive), got %v", where, s.Objective))
		}
		if s.Window == 0 {
			errs = append(errs, fmt.Errorf("%s: window is required (e.g. 30d)", where))
		}
		for field, q := range map[string]string{"sli.errors": s.SLI.Errors, "sli.total": s.SLI.Total} {
			if !strings.Contains(q, WindowPlaceholder) {
				errs = append(errs, fmt.Errorf("%s: %s must contain %s", where, field, WindowPlaceholder))
			}
		}
	}
	return errors.Join(errs...)
}

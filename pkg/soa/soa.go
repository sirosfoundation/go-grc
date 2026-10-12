// Package soa loads and validates Statements of Applicability.
//
// A Statement of Applicability belongs to one audit cycle and lives at
// "<audits dir>/<cycle>/soa.yaml", next to that cycle's "audit.yaml"
// (findings) and "references/" (supporting material that is never rendered).
package soa

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Applicability is true, false or partial. YAML booleans are accepted.
type Applicability string

const (
	Applicable    Applicability = "true"
	NotApplicable Applicability = "false"
	Partial       Applicability = "partial"
)

// UnmarshalYAML accepts a YAML bool or the string "partial".
func (a *Applicability) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: applicable must be true, false or partial", n.Line)
	}
	*a = Applicability(n.Value)
	return nil
}

// Header identifies the statement.
type Header struct {
	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	ScopeRef  string `yaml:"scope_ref"`
	Date      string `yaml:"date"`
	Version   string `yaml:"version"`
	Assurance string `yaml:"assurance"`
}

// Requirement is one row of the statement.
type Requirement struct {
	AnnexA        string        `yaml:"annex_a"`
	Title         string        `yaml:"title"`
	Applicable    Applicability `yaml:"applicable"`
	Status        string        `yaml:"status"`
	Justification string        `yaml:"justification"`
	Evidence      []string      `yaml:"evidence,omitempty"`
}

// SoA is a Statement of Applicability.
type SoA struct {
	Header       Header        `yaml:"soa"`
	Requirements []Requirement `yaml:"requirements"`
}

// Loaded is a statement together with where it came from.
type Loaded struct {
	// Cycle is the audit cycle directory name, e.g. "iso27001-devprocess-2026-07".
	Cycle string
	Path  string
	SoA   SoA
}

// Load reads "<auditsDir>/<cycle>/soa.yaml" for every cycle directory that
// has one, ordered by cycle name. A missing auditsDir yields none.
func Load(auditsDir string) ([]*Loaded, error) {
	entries, err := os.ReadDir(auditsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading audits dir: %w", err)
	}
	var out []*Loaded
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(auditsDir, e.Name(), "soa.yaml")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var s SoA
		if err := yaml.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		out = append(out, &Loaded{Cycle: e.Name(), Path: path, SoA: s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cycle < out[j].Cycle })
	return out, nil
}

var validStatus = map[string]bool{"implemented": true, "partial": true, "gap": true}

// Validate returns a description of every problem in the statement.
func (s *SoA) Validate() []string {
	var problems []string
	if s.Header.ID == "" {
		problems = append(problems, "soa.id is empty")
	}
	if len(s.Requirements) == 0 {
		problems = append(problems, "no requirements")
	}
	seen := make(map[string]bool)
	for _, r := range s.Requirements {
		switch {
		case r.AnnexA == "":
			problems = append(problems, "requirement without annex_a")
			continue
		case seen[r.AnnexA]:
			problems = append(problems, fmt.Sprintf("%s: duplicate requirement", r.AnnexA))
		}
		seen[r.AnnexA] = true
		switch r.Applicable {
		case Applicable, NotApplicable, Partial:
		default:
			problems = append(problems, fmt.Sprintf("%s: applicable is %q, want true, false or partial", r.AnnexA, r.Applicable))
		}
		if !validStatus[r.Status] {
			problems = append(problems, fmt.Sprintf("%s: status is %q, want implemented, partial or gap", r.AnnexA, r.Status))
		}
		if r.Justification == "" {
			problems = append(problems, fmt.Sprintf("%s: no justification", r.AnnexA))
		}
	}
	return problems
}

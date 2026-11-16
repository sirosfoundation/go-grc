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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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

// Lifecycle states of a statement. Only an approved statement is published;
// every other state, including a missing or unknown one, is internal.
const (
	StatusDraft    = "draft"
	StatusInReview = "in_review"
	StatusApproved = "approved"
)

// Header identifies the statement.
type Header struct {
	// Status is the review state: draft, in_review or approved. Absent means
	// draft, so a statement is never published by omission.
	Status string `yaml:"status"`
	// ApprovedDate is when the review finished; required once approved.
	ApprovedDate string `yaml:"approved_date,omitempty"`

	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	ScopeRef  string `yaml:"scope_ref"`
	Date      string `yaml:"date"`
	Version   string `yaml:"version"`
	Assurance string `yaml:"assurance"`
}

// Requirement is one row of the statement.
type Requirement struct {
	AnnexA string `yaml:"annex_a"`
	Title  string `yaml:"title"`
	// Section optionally groups the requirement on the matrix page. Absent,
	// ISO 27001 Annex A ids are grouped by theme and anything else is listed
	// ungrouped.
	Section       string        `yaml:"section,omitempty"`
	Applicable    Applicability `yaml:"applicable"`
	Status        string        `yaml:"status"`
	Justification string        `yaml:"justification"`
	Evidence      []string      `yaml:"evidence,omitempty"`
	// Activity, Owner, TargetDate and Issue are the plan for closing a
	// requirement that is partial or a gap: what will be done, by whom (a
	// role), by when (YYYY-MM-DD) and the tracking issue ("org/repo#n"). All
	// are optional; see NeedsPlan and Warnings for when they are expected.
	Activity   string `yaml:"activity,omitempty"`
	Owner      string `yaml:"owner,omitempty"`
	TargetDate string `yaml:"target_date,omitempty"`
	Issue      string `yaml:"issue,omitempty"`
	// BroaderScope is an optional second assessment of the same control
	// against a wider scope (for example including the deployment platform),
	// shown side by side with the main one. Absent for most statements.
	BroaderScope *BroaderScope `yaml:"broader_scope,omitempty"`
}

// BroaderScope is the optional assessment of a control against a broader
// scope. Every field is optional.
type BroaderScope struct {
	Applicable Applicability `yaml:"applicable,omitempty"`
	Status     string        `yaml:"status,omitempty"`
	// Delta says how the broader scope differs from the main one.
	Delta string `yaml:"delta,omitempty"`
	Note  string `yaml:"note,omitempty"`
}

// Deltas a broader scope may declare.
const (
	DeltaNone              = "none"
	DeltaNewGap            = "new-gap"
	DeltaScopeWidens       = "scope-widens"
	DeltaNewEvidenceNeeded = "new-evidence-needed"
	DeltaInheritedSupplier = "inherited-from-supplier"
)

var validDelta = map[string]bool{
	DeltaNone: true, DeltaNewGap: true, DeltaScopeWidens: true,
	DeltaNewEvidenceNeeded: true, DeltaInheritedSupplier: true,
}

// NeedsPlan reports whether the requirement applies and is not fully
// implemented, so it is expected to carry an activity, an owner and a date.
func (r Requirement) NeedsPlan() bool {
	return r.Applicable != NotApplicable && (r.Status == "partial" || r.Status == "gap")
}

// HasPlan reports whether any requirement carries plan fields.
func (s *SoA) HasPlan() bool {
	for _, r := range s.Requirements {
		if r.Activity != "" || r.Owner != "" || r.TargetDate != "" || r.Issue != "" {
			return true
		}
	}
	return false
}

var issueRe = regexp.MustCompile(`^([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)#([0-9]+)$`)

// ParseIssue splits "org/repo#n" into the repository and the number.
func ParseIssue(s string) (repo string, number int, ok bool) {
	m := issueRe.FindStringSubmatch(s)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return m[1], n, true
}

// ParseDate parses a target date written YYYY-MM-DD.
func ParseDate(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// planRequired is true once the statement has left draft: the plan fields are
// then errors when missing, and warnings while the statement is a draft.
func (s *SoA) planRequired() bool {
	return s.Header.Status == StatusInReview || s.Header.Status == StatusApproved
}

// planGaps lists, per requirement that needs a plan, which fields are missing.
func (s *SoA) planGaps() []string {
	var out []string
	for _, r := range s.Requirements {
		if !r.NeedsPlan() {
			continue
		}
		var missing []string
		if strings.TrimSpace(r.Activity) == "" {
			missing = append(missing, "activity")
		}
		if strings.TrimSpace(r.Owner) == "" {
			missing = append(missing, "owner")
		}
		if strings.TrimSpace(r.TargetDate) == "" {
			missing = append(missing, "target_date")
		}
		if len(missing) > 0 {
			out = append(out, fmt.Sprintf("%s: is %s and has no %s", r.AnnexA, r.Status, joinWords(missing)))
		}
	}
	return out
}

func joinWords(w []string) string {
	switch len(w) {
	case 1:
		return w[0]
	case 2:
		return w[0] + " or " + w[1]
	}
	return w[0] + ", " + w[1] + " or " + w[2]
}

// Warnings reports what is merely incomplete: while the statement is a draft,
// the missing plan fields of partial and gap requirements. Once it is in
// review or approved they are errors from Validate instead.
func (s *SoA) Warnings() []string {
	if s.planRequired() {
		return nil
	}
	return s.planGaps()
}

// HasBroaderScope reports whether any requirement carries a broader scope.
func (s *SoA) HasBroaderScope() bool {
	for _, r := range s.Requirements {
		if r.BroaderScope != nil {
			return true
		}
	}
	return false
}

// SoA is a Statement of Applicability.
type SoA struct {
	Header       Header        `yaml:"soa"`
	Requirements []Requirement `yaml:"requirements"`
}

// Published reports whether the statement may be shown publicly: only once
// its review is finished. Anything else, including an unrecognized status,
// is internal.
func (s *SoA) Published() bool { return s.Header.Status == StatusApproved }

// State returns the lifecycle status, defaulting to draft.
func (s *SoA) State() string {
	if s.Header.Status == "" {
		return StatusDraft
	}
	return s.Header.Status
}

// Loaded is a statement together with where it came from.
type Loaded struct {
	// Kind is KindAudit for a real audit's statement and KindAssessment for an
	// internal assessment's. Assessment statements are never public.
	Kind string
	// Cycle is the audit (or assessment) directory name, e.g.
	// "iso27001-devprocess-2026-07".
	Cycle string
	Path  string
	SoA   SoA
}

const (
	KindAudit      = "audit"
	KindAssessment = "assessment"
)

// LoadAll reads the statements of real audits and of internal assessments,
// audits first.
func LoadAll(auditsDir, assessmentsDir string) ([]*Loaded, error) {
	out, err := LoadDir(auditsDir, KindAudit)
	if err != nil {
		return nil, err
	}
	if assessmentsDir == "" {
		return out, nil
	}
	more, err := LoadDir(assessmentsDir, KindAssessment)
	if err != nil {
		return nil, err
	}
	return append(out, more...), nil
}

// Load reads the statements of real audits.
func Load(auditsDir string) ([]*Loaded, error) { return LoadDir(auditsDir, KindAudit) }

// LoadDir reads "<dir>/<name>/soa.yaml" for every subdirectory that has one,
// ordered by name. A missing dir yields none.
func LoadDir(auditsDir, kind string) ([]*Loaded, error) {
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
		out = append(out, &Loaded{Kind: kind, Cycle: e.Name(), Path: path, SoA: s})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cycle < out[j].Cycle })
	return out, nil
}

var validStatus = map[string]bool{"implemented": true, "partial": true, "gap": true, "not_applicable": true}

// Validate returns a description of every problem in the statement.
func (s *SoA) Validate() []string {
	var problems []string
	if s.Header.ID == "" {
		problems = append(problems, "soa.id is empty")
	}
	switch s.Header.Status {
	case "", StatusDraft, StatusInReview:
	case StatusApproved:
		if s.Header.ApprovedDate == "" {
			problems = append(problems, "status is approved but approved_date is empty")
		}
	default:
		problems = append(problems, fmt.Sprintf("soa.status is %q, want draft, in_review or approved", s.Header.Status))
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
			problems = append(problems, fmt.Sprintf("%s: status is %q, want implemented, partial, gap or not_applicable", r.AnnexA, r.Status))
		}
		if r.Justification == "" {
			problems = append(problems, fmt.Sprintf("%s: no justification", r.AnnexA))
		}
		if r.TargetDate != "" {
			if _, ok := ParseDate(r.TargetDate); !ok {
				problems = append(problems, fmt.Sprintf("%s: target_date is %q, want YYYY-MM-DD", r.AnnexA, r.TargetDate))
			}
		}
		if r.Issue != "" {
			if _, _, ok := ParseIssue(r.Issue); !ok {
				problems = append(problems, fmt.Sprintf("%s: issue is %q, want org/repo#number", r.AnnexA, r.Issue))
			}
		}
		if b := r.BroaderScope; b != nil {
			problems = append(problems, b.validate(r.AnnexA)...)
		}
	}
	if s.planRequired() {
		problems = append(problems, s.planGaps()...)
	}
	return problems
}

// validate reports bad values only; every field may be absent.
func (b *BroaderScope) validate(id string) []string {
	var problems []string
	switch b.Applicable {
	case "", Applicable, NotApplicable, Partial:
	default:
		problems = append(problems, fmt.Sprintf("%s: broader_scope.applicable is %q, want true, false or partial", id, b.Applicable))
	}
	if b.Status != "" && !validStatus[b.Status] {
		problems = append(problems, fmt.Sprintf("%s: broader_scope.status is %q, want implemented, partial, gap or not_applicable", id, b.Status))
	}
	if b.Delta != "" && !validDelta[b.Delta] {
		problems = append(problems, fmt.Sprintf("%s: broader_scope.delta is %q, want none, new-gap, scope-widens, new-evidence-needed or inherited-from-supplier", id, b.Delta))
	}
	return problems
}

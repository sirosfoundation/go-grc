package soa_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/soa"
)

const sample = `soa:
  id: SOA-1
  title: T
  version: "1"
requirements:
- annex_a: A.5.1
  title: Policies
  applicable: true
  status: implemented
  justification: because
  evidence: [policies/x.md]
- annex_a: A.5.2
  title: Roles
  applicable: partial
  status: gap
  justification: reduced scope
- annex_a: A.7.1
  title: Physical
  applicable: false
  status: gap
  justification: remote only
`

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAndValidate(t *testing.T) {
	root := t.TempDir()
	write(t, root, "b-cycle/soa.yaml", sample)
	write(t, root, "a-cycle/soa.yaml", sample)
	write(t, root, "no-soa/audit.yaml", "audit: {}\n")
	write(t, root, "flat.yaml", "audit: {}\n")

	got, err := soa.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Cycle != "a-cycle" || got[1].Cycle != "b-cycle" {
		t.Fatalf("loaded %d statements, want a-cycle then b-cycle", len(got))
	}
	rs := got[0].SoA.Requirements
	if rs[0].Applicable != soa.Applicable || rs[1].Applicable != soa.Partial || rs[2].Applicable != soa.NotApplicable {
		t.Errorf("applicability = %q %q %q", rs[0].Applicable, rs[1].Applicable, rs[2].Applicable)
	}
	if p := got[0].SoA.Validate(); len(p) != 0 {
		t.Errorf("valid statement reported problems: %v", p)
	}
}

func TestLoadMissingDir(t *testing.T) {
	got, err := soa.Load(filepath.Join(t.TempDir(), "nope"))
	if err != nil || got != nil {
		t.Errorf("Load(missing) = %v, %v; want nil, nil", got, err)
	}
}

func TestValidateProblems(t *testing.T) {
	s := soa.SoA{Requirements: []soa.Requirement{
		{AnnexA: "A.1", Applicable: "maybe", Status: "done"},
		{AnnexA: "A.1", Applicable: soa.Applicable, Status: "gap", Justification: "j"},
		{},
	}}
	joined := strings.Join(s.Validate(), "\n")
	for _, want := range []string{"soa.id is empty", `applicable is "maybe"`, `status is "done"`, "no justification", "duplicate requirement", "requirement without annex_a"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Validate() missing %q in:\n%s", want, joined)
		}
	}
}

func TestPublishedOnlyWhenApproved(t *testing.T) {
	for status, want := range map[string]bool{"": false, "draft": false, "in_review": false, "approved": true, "Approved": false, "published": false} {
		s := soa.SoA{Header: soa.Header{Status: status}}
		if got := s.Published(); got != want {
			t.Errorf("Published(%q) = %v, want %v", status, got, want)
		}
	}
	if (&soa.SoA{}).State() != soa.StatusDraft {
		t.Error("missing status must read as draft")
	}
}

func TestValidateStatus(t *testing.T) {
	ok := soa.Requirement{AnnexA: "A.1", Applicable: soa.Applicable, Status: "gap", Justification: "j", Activity: "a", Owner: "o", TargetDate: "2026-12-01"}
	cases := map[string]string{
		"approved without a date": "approved_date is empty",
		"bogus":                   `soa.status is "bogus"`,
	}
	for status, want := range cases {
		st := status
		if st == "approved without a date" {
			st = "approved"
		}
		s := soa.SoA{Header: soa.Header{ID: "S", Status: st}, Requirements: []soa.Requirement{ok}}
		if joined := strings.Join(s.Validate(), "\n"); !strings.Contains(joined, want) {
			t.Errorf("status %q: want %q in %q", status, want, joined)
		}
	}
	s := soa.SoA{Header: soa.Header{ID: "S", Status: "approved", ApprovedDate: "2026-10-20"}, Requirements: []soa.Requirement{ok}}
	if p := s.Validate(); len(p) != 0 {
		t.Errorf("approved with date reported %v", p)
	}
}

func TestValidateAcceptsNotApplicableStatus(t *testing.T) {
	s := soa.SoA{Header: soa.Header{ID: "S"}, Requirements: []soa.Requirement{
		{AnnexA: "A.1", Applicable: soa.NotApplicable, Status: "not_applicable", Justification: "j"},
	}}
	if p := s.Validate(); len(p) != 0 {
		t.Errorf("not_applicable status rejected: %v", p)
	}
}

func TestValidateBroaderScope(t *testing.T) {
	cases := []struct {
		name string
		b    *soa.BroaderScope
		want string // empty: no problem
	}{
		{"absent", nil, ""},
		{"empty", &soa.BroaderScope{}, ""},
		{"all valid", &soa.BroaderScope{Applicable: soa.Partial, Status: "gap", Delta: "new-gap", Note: "n"}, ""},
		{"each delta ok", &soa.BroaderScope{Delta: "inherited-from-supplier"}, ""},
		{"bad applicable", &soa.BroaderScope{Applicable: "maybe"}, `broader_scope.applicable is "maybe"`},
		{"bad status", &soa.BroaderScope{Status: "done"}, `broader_scope.status is "done"`},
		{"bad delta", &soa.BroaderScope{Delta: "worse"}, `broader_scope.delta is "worse"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := soa.SoA{Header: soa.Header{ID: "S"}, Requirements: []soa.Requirement{
				{AnnexA: "A.1", Applicable: soa.Applicable, Status: "gap", Justification: "j", BroaderScope: c.b},
			}}
			joined := strings.Join(s.Validate(), "\n")
			if c.want == "" && joined != "" {
				t.Errorf("unexpected problems: %s", joined)
			}
			if c.want != "" && !strings.Contains(joined, c.want) {
				t.Errorf("want %q in %q", c.want, joined)
			}
		})
	}
}

func TestBroaderScopeLoadsFromYAML(t *testing.T) {
	root := t.TempDir()
	write(t, root, "c/soa.yaml", sample+`- annex_a: A.8.1
  title: Endpoints
  applicable: true
  status: implemented
  justification: j
  broader_scope: {applicable: partial, status: gap, delta: new-gap, note: platform}
`)
	got, err := soa.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := got[0].SoA
	if !s.HasBroaderScope() {
		t.Fatal("HasBroaderScope = false")
	}
	bs := s.Requirements[3].BroaderScope
	if bs == nil || bs.Applicable != soa.Partial || bs.Status != "gap" || bs.Delta != "new-gap" || bs.Note != "platform" {
		t.Errorf("broader_scope = %+v", bs)
	}
	if s.Requirements[0].BroaderScope != nil {
		t.Error("requirement without broader_scope got one")
	}
}

func TestPlanFieldsLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	// target_date is deliberately unquoted: YAML reads it as a timestamp.
	y := `soa:
  id: S
requirements:
- annex_a: A.5.1
  applicable: true
  status: partial
  justification: j
  activity: Approve the policy
  owner: CISO
  target_date: 2026-12-01
  issue: org/repo#12
`
	if err := os.WriteFile(filepath.Join(dir, "c", "soa.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	all, err := soa.LoadAll(dir, filepath.Join(dir, "none"))
	if err != nil || len(all) != 1 {
		t.Fatalf("LoadAll: %v %d", err, len(all))
	}
	r := all[0].SoA.Requirements[0]
	if r.Activity != "Approve the policy" || r.Owner != "CISO" || r.TargetDate != "2026-12-01" || r.Issue != "org/repo#12" {
		t.Errorf("plan fields = %+v", r)
	}
	if p := all[0].SoA.Validate(); len(p) != 0 {
		t.Errorf("Validate() = %v", p)
	}
	if !all[0].SoA.HasPlan() {
		t.Error("HasPlan() = false")
	}
}

func TestNeedsPlan(t *testing.T) {
	cases := []struct {
		app    soa.Applicability
		status string
		want   bool
	}{
		{soa.Applicable, "gap", true},
		{soa.Applicable, "partial", true},
		{soa.Partial, "gap", true},
		{soa.Applicable, "implemented", false},
		{soa.NotApplicable, "gap", false},
		{soa.NotApplicable, "not_applicable", false},
	}
	for _, c := range cases {
		if got := (soa.Requirement{Applicable: c.app, Status: c.status}).NeedsPlan(); got != c.want {
			t.Errorf("NeedsPlan(%s, %s) = %v, want %v", c.app, c.status, got, c.want)
		}
	}
}

func TestPlanFormatErrors(t *testing.T) {
	s := soa.SoA{Header: soa.Header{ID: "S"}, Requirements: []soa.Requirement{
		{AnnexA: "A.1", Applicable: soa.Applicable, Status: "implemented", Justification: "j", TargetDate: "1 Dec", Issue: "#4"},
		{AnnexA: "A.2", Applicable: soa.Applicable, Status: "implemented", Justification: "j", TargetDate: "2026-02-30", Issue: "org/repo#0"},
	}}
	joined := strings.Join(s.Validate(), "\n")
	for _, want := range []string{`A.1: target_date is "1 Dec"`, `A.1: issue is "#4"`, `A.2: target_date is "2026-02-30"`, `A.2: issue is "org/repo#0"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestPlanRequiredOnceInReview(t *testing.T) {
	req := []soa.Requirement{
		{AnnexA: "A.1", Applicable: soa.Applicable, Status: "gap", Justification: "j"},
		{AnnexA: "A.2", Applicable: soa.Applicable, Status: "partial", Justification: "j", Activity: "x", Owner: "CISO"},
		{AnnexA: "A.3", Applicable: soa.Applicable, Status: "implemented", Justification: "j"},
		{AnnexA: "A.4", Applicable: soa.Applicable, Status: "gap", Justification: "j", Activity: "x", Owner: "CISO", TargetDate: "2026-12-01"},
	}
	for _, status := range []string{"", "draft"} {
		s := soa.SoA{Header: soa.Header{ID: "S", Status: status}, Requirements: req}
		if p := s.Validate(); len(p) != 0 {
			t.Errorf("%q: Validate() = %v, want none", status, p)
		}
		w := strings.Join(s.Warnings(), "\n")
		for _, want := range []string{"A.1: is gap and has no activity, owner or target_date", "A.2: is partial and has no target_date"} {
			if !strings.Contains(w, want) {
				t.Errorf("%q: missing warning %q in:\n%s", status, want, w)
			}
		}
		if strings.Contains(w, "A.3") || strings.Contains(w, "A.4") {
			t.Errorf("%q: unexpected warning in:\n%s", status, w)
		}
	}
	for _, status := range []string{"in_review", "approved"} {
		s := soa.SoA{Header: soa.Header{ID: "S", Status: status, ApprovedDate: "2026-11-01"}, Requirements: req}
		p := strings.Join(s.Validate(), "\n")
		if !strings.Contains(p, "A.1: is gap and has no activity, owner or target_date") || !strings.Contains(p, "A.2: is partial and has no target_date") {
			t.Errorf("%q: Validate() = %s", status, p)
		}
		if len(s.Warnings()) != 0 {
			t.Errorf("%q: Warnings() = %v, want none", status, s.Warnings())
		}
	}
}

func TestWhitespaceOnlyPlanFieldsAreMissing(t *testing.T) {
	s := soa.SoA{Header: soa.Header{ID: "S", Status: "approved", ApprovedDate: "2026-11-01"}, Requirements: []soa.Requirement{
		{AnnexA: "A.1", Applicable: soa.Applicable, Status: "gap", Justification: "j", Activity: "  ", Owner: "\t", TargetDate: "2026-12-01"},
	}}
	if p := strings.Join(s.Validate(), "\n"); !strings.Contains(p, "A.1: is gap and has no activity or owner") {
		t.Errorf("Validate() = %q", p)
	}
}

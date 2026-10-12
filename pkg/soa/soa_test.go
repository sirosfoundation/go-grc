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
	ok := soa.Requirement{AnnexA: "A.1", Applicable: soa.Applicable, Status: "gap", Justification: "j"}
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

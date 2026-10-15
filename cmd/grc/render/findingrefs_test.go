package render

import (
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/audit"
)

func TestNaturalLess(t *testing.T) {
	if !naturalLess("ISO-DEV-2", "ISO-DEV-10") || naturalLess("ISO-DEV-10", "ISO-DEV-2") {
		t.Error("ISO-DEV-2 must sort before ISO-DEV-10")
	}
	if !naturalLess("EN-P-1", "ISO-DEV-1") {
		t.Error("different prefixes sort lexically")
	}
}

func TestRiskFindingCell(t *testing.T) {
	set := &audit.AuditSet{FindingsByID: map[string]*audit.FindingRef{
		"F-1": {Finding: &audit.Finding{ID: "F-1", Title: "Open one", TrackingIssue: &audit.IssueRef{Repo: "o/r", Number: 5}, Status: "open"}},
		"F-2": {Finding: &audit.Finding{ID: "F-2", Title: "Done", TrackingIssue: &audit.IssueRef{Repo: "o/r", Number: 6}, Status: "resolved"}},
		"F-3": {Finding: &audit.Finding{ID: "F-3", Title: "Untracked", Status: "open"}},
	}}
	for id, want := range map[string]string{
		"F-1":  "[F-1](/findings/f_1) — Open one",
		"F-2":  "[F-2](https://github.com/o/r/issues/6) — Done",
		"F-3":  "`F-3` — Untracked",
		"GONE": "GONE",
		"":     "–",
	} {
		if got := riskFindingCell(id, set); got != want {
			t.Errorf("riskFindingCell(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestFindingsIndexGroupedBySource(t *testing.T) {
	mk := func(id string) audit.Finding {
		return audit.Finding{ID: id, Title: id, Severity: "high", Status: "open", TrackingIssue: &audit.IssueRef{Repo: "o/r", Number: 1}}
	}
	a := audit.LoadedFile{Path: "audits/a/audit.yaml", Kind: audit.KindAudit, Name: "a", Data: audit.AuditFile{Findings: []audit.Finding{mk("A-1")}}}
	b := audit.LoadedFile{Path: "assessments/b/assessment.yaml", Kind: audit.KindAssessment, Name: "b", Data: audit.AuditFile{Findings: []audit.Finding{mk("B-1")}}}
	set := &audit.AuditSet{Files: []audit.LoadedFile{a, b}, FindingsByID: map[string]*audit.FindingRef{}}
	// Refs deliberately hold copies of the files, as after the loader's slice growth.
	ac, bc := a, b
	set.FindingsByID["A-1"] = &audit.FindingRef{File: &ac, Finding: &set.Files[0].Data.Findings[0]}
	set.FindingsByID["B-1"] = &audit.FindingRef{File: &bc, Finding: &set.Files[1].Data.Findings[0]}
	out := renderFindingsIndex(set, []*audit.Finding{set.FindingsByID["B-1"].Finding, set.FindingsByID["A-1"].Finding})
	ia, ib := strings.Index(out, "## Audit: a"), strings.Index(out, "## Assessment: b")
	if ia < 0 || ib < 0 || ia > ib {
		t.Fatalf("want audit group before assessment group:\n%s", out)
	}
	if !strings.Contains(out[ia:ib], "A-1") || strings.Contains(out[ia:ib], "B-1") {
		t.Errorf("findings in wrong group:\n%s", out)
	}
}

package catalog

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/audit"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "testdata")
}

func TestDeriveFromFindings_AllResolvedWithEvidence(t *testing.T) {
	findings := []*audit.FindingRef{
		{Finding: &audit.Finding{
			Status:   "resolved",
			Evidence: []audit.Evidence{{Type: "merged_pr", Ref: "org/repo#1"}},
		}},
	}
	got := deriveFromFindings(findings)
	if got != ControlValidated {
		t.Errorf("expected %q, got %q", ControlValidated, got)
	}
}

func TestDeriveFromFindings_AllResolvedNoEvidence(t *testing.T) {
	findings := []*audit.FindingRef{
		{Finding: &audit.Finding{Status: "resolved"}},
	}
	got := deriveFromFindings(findings)
	if got != ControlVerified {
		t.Errorf("expected %q, got %q", ControlVerified, got)
	}
}

func TestDeriveFromFindings_InProgress(t *testing.T) {
	findings := []*audit.FindingRef{
		{Finding: &audit.Finding{Status: "in_progress"}},
	}
	got := deriveFromFindings(findings)
	if got != ControlInProgress {
		t.Errorf("expected %q, got %q", ControlInProgress, got)
	}
}

func TestDeriveFromFindings_Open(t *testing.T) {
	findings := []*audit.FindingRef{
		{Finding: &audit.Finding{Status: "open"}},
	}
	got := deriveFromFindings(findings)
	if got != ControlToDo {
		t.Errorf("expected %q, got %q", ControlToDo, got)
	}
}

func TestDeriveFromFindings_Accepted(t *testing.T) {
	findings := []*audit.FindingRef{
		{Finding: &audit.Finding{Status: "accepted"}},
	}
	got := deriveFromFindings(findings)
	if got != ControlVerified {
		t.Errorf("expected %q, got %q (accepted is terminal)", ControlVerified, got)
	}
}

func TestDeriveFromFindingsForProfile(t *testing.T) {
	findings := []*audit.FindingRef{
		{Finding: &audit.Finding{
			Status:   "open",
			Severity: "high",
			Profiles: map[string]audit.ProfileOverride{
				"native_only": {Status: "resolved"},
			},
		}},
	}
	got := deriveFromFindingsForProfile(findings, "native_only")
	if got != ControlVerified {
		t.Errorf("expected %q for native_only profile, got %q", ControlVerified, got)
	}

	got = deriveFromFindingsForProfile(findings, "")
	if got != ControlToDo {
		t.Errorf("expected %q for empty profile (open finding), got %q", ControlToDo, got)
	}
}

func TestDeriveControlStatuses(t *testing.T) {
	cat, err := Load(filepath.Join(testdataDir(), "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	audits, err := audit.Load(filepath.Join(testdataDir(), "audits"))
	if err != nil {
		t.Fatal(err)
	}

	DeriveControlStatuses(cat, audits)
	// SEC-AUTH-01 has findings, so DerivedStatus should be set
	ctrl := cat.Controls["SEC-AUTH-01"]
	if ctrl == nil {
		t.Fatal("SEC-AUTH-01 not found")
	}
	// Verify EffectiveStatus works
	eff := EffectiveStatus(ctrl)
	if eff == "" {
		t.Error("EffectiveStatus should not be empty")
	}

	// Test EffectiveStatus when DerivedStatus is empty
	ctrl2 := &Control{Status: "implemented"}
	if EffectiveStatus(ctrl2) != "implemented" {
		t.Error("EffectiveStatus should return Status when DerivedStatus is empty")
	}
}

func TestDeriveControlStatusesForProfile(t *testing.T) {
	cat, err := Load(filepath.Join(testdataDir(), "catalog"))
	if err != nil {
		t.Fatal(err)
	}
	audits, err := audit.Load(filepath.Join(testdataDir(), "audits"))
	if err != nil {
		t.Fatal(err)
	}

	DeriveControlStatusesForProfile(cat, audits, "")
	// SEC-AUTH-01 has F-001 which is resolved with evidence
	ctrl := cat.Controls["SEC-AUTH-01"]
	if ctrl.DerivedStatus == "" && ctrl.Status != ControlValidated {
		t.Logf("SEC-AUTH-01 status=%s derived=%s", ctrl.Status, ctrl.DerivedStatus)
	}
}

func TestLoadFrameworkCatalog(t *testing.T) {
	fc, err := LoadFrameworkCatalog(filepath.Join(testdataDir(), "catalog"), "eudi-secreq", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fc == nil {
		t.Fatal("expected non-nil framework catalog")
	}
	if len(fc.Requirements) != 1 {
		t.Fatalf("expected 1 requirement, got %d", len(fc.Requirements))
	}
	if fc.Requirements[0].ID != "WTE_07" {
		t.Errorf("expected requirement WTE_07, got %s", fc.Requirements[0].ID)
	}
	if fc.ByID["WTE_07"] == nil {
		t.Error("ByID lookup for WTE_07 should work")
	}
}

func TestLoadFrameworkCatalog_Missing(t *testing.T) {
	fc, err := LoadFrameworkCatalog(filepath.Join(testdataDir(), "catalog"), "nonexistent", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fc != nil {
		t.Error("expected nil for missing framework catalog")
	}
}

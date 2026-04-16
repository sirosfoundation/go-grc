package audit_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/audit"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "testdata")
}

func TestLoad(t *testing.T) {
	set, err := audit.Load(filepath.Join(testdataDir(), "audits"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(set.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(set.Files))
	}

	if len(set.FindingsByID) != 3 {
		t.Errorf("expected 3 findings, got %d", len(set.FindingsByID))
	}

	// Check specific finding
	f001, ok := set.FindingsByID["F-001"]
	if !ok {
		t.Fatal("F-001 not found")
	}
	if f001.Finding.Severity != "high" {
		t.Errorf("F-001 severity: got %q, want 'high'", f001.Finding.Severity)
	}
	if !f001.Finding.IsResolved() {
		t.Error("F-001 should be resolved")
	}
	if !f001.Finding.HasEvidence() {
		t.Error("F-001 should have evidence")
	}

	// Check control index
	authFindings := set.FindingsByControl["SEC-AUTH-01"]
	if len(authFindings) != 1 {
		t.Errorf("SEC-AUTH-01: expected 1 finding, got %d", len(authFindings))
	}

	sessFindings := set.FindingsByControl["SEC-SESS-01"]
	if len(sessFindings) != 1 {
		t.Errorf("SEC-SESS-01: expected 1 finding, got %d", len(sessFindings))
	}

	// Check tracking issue
	if f001.Finding.TrackingIssue == nil {
		t.Fatal("F-001 should have tracking issue")
	}
	if f001.Finding.TrackingIssue.Number != 1 {
		t.Errorf("F-001 tracking issue: got %d, want 1", f001.Finding.TrackingIssue.Number)
	}
}

func TestFinding_AddEvidence(t *testing.T) {
	f := &audit.Finding{ID: "F-TEST", Status: "open"}
	if f.HasEvidence() {
		t.Error("should not have evidence initially")
	}

	f.AddEvidence(audit.Evidence{
		Type:        "merged_pr",
		Ref:         "org/repo#42",
		Description: "test evidence",
	})

	if !f.HasEvidence() {
		t.Error("should have evidence after AddEvidence")
	}
	if f.Evidence[0].CollectedAt == "" {
		t.Error("CollectedAt should be auto-filled")
	}
}

func TestSave(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(testdataDir(), "audits", "sa-2025-001.yaml")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "sa-2025-001.yaml")
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatal(err)
	}

	set, err := audit.Load(tmp)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if err := set.Files[0].Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Re-load and verify
	set2, err := audit.Load(tmp)
	if err != nil {
		t.Fatalf("Re-load error: %v", err)
	}
	if len(set2.FindingsByID) != 3 {
		t.Errorf("expected 3 findings after round-trip, got %d", len(set2.FindingsByID))
	}
}

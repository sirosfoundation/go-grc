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

func TestFinding_MatchesReq(t *testing.T) {
	tests := []struct {
		name   string
		f      audit.Finding
		fwID   string
		reqID  string
		expect bool
	}{
		{
			name:   "generic FrameworkRefs match",
			f:      audit.Finding{FrameworkRefs: map[string][]string{"eudi": {"SR-01", "SR-02"}}},
			fwID:   "eudi",
			reqID:  "SR-02",
			expect: true,
		},
		{
			name:   "generic FrameworkRefs miss",
			f:      audit.Finding{FrameworkRefs: map[string][]string{"eudi": {"SR-01"}}},
			fwID:   "eudi",
			reqID:  "SR-99",
			expect: false,
		},
		{
			name:   "legacy EUDIReqs match",
			f:      audit.Finding{EUDIReqs: []string{"SR-01"}},
			fwID:   "eudi",
			reqID:  "SR-01",
			expect: true,
		},
		{
			name:   "legacy AnnexA match",
			f:      audit.Finding{AnnexA: []string{"A.5.1"}},
			fwID:   "iso27001",
			reqID:  "A.5.1",
			expect: true,
		},
		{
			name:   "legacy GDPRItems match",
			f:      audit.Finding{GDPRItems: []string{"art5"}},
			fwID:   "gdpr",
			reqID:  "art5",
			expect: true,
		},
		{
			name:   "legacy ASVSSections match",
			f:      audit.Finding{ASVSSections: []string{"V2.1"}},
			fwID:   "owasp-asvs",
			reqID:  "V2.1",
			expect: true,
		},
		{
			name:   "unknown framework no match",
			f:      audit.Finding{},
			fwID:   "unknown",
			reqID:  "anything",
			expect: false,
		},
		{
			name:   "generic overrides legacy",
			f:      audit.Finding{FrameworkRefs: map[string][]string{"eudi": {"SR-X"}}, EUDIReqs: []string{"SR-01"}},
			fwID:   "eudi",
			reqID:  "SR-X",
			expect: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.f.MatchesReq(tt.fwID, tt.reqID)
			if got != tt.expect {
				t.Errorf("MatchesReq(%q, %q) = %v, want %v", tt.fwID, tt.reqID, got, tt.expect)
			}
		})
	}
}

func TestFinding_StatusHelpers(t *testing.T) {
	f := &audit.Finding{Status: "open"}
	if f.IsResolved() {
		t.Error("open finding should not be resolved")
	}
	if f.IsTerminal() {
		t.Error("open finding should not be terminal")
	}
	if f.IsActive() {
		t.Error("open finding should not be active")
	}

	f.Status = "in_progress"
	if f.IsTerminal() {
		t.Error("in_progress finding should not be terminal")
	}
	if !f.IsActive() {
		t.Error("in_progress finding should be active")
	}

	f.Status = "resolved"
	if !f.IsResolved() {
		t.Error("resolved finding should be resolved")
	}
	if !f.IsTerminal() {
		t.Error("resolved finding should be terminal")
	}

	f.Status = "accepted"
	if !f.IsTerminal() {
		t.Error("accepted finding should be terminal")
	}
	if f.IsResolved() {
		t.Error("accepted finding should not be resolved (IsResolved is for status==resolved only)")
	}
}

func TestFinding_ProfileOverrides(t *testing.T) {
	f := &audit.Finding{
		ID:       "F-PROF",
		Status:   "open",
		Severity: "high",
		Evidence: []audit.Evidence{
			{Type: "document", Ref: "base-doc", Description: "base evidence"},
		},
		Profiles: map[string]audit.ProfileOverride{
			"native_only": {
				Status:   "resolved",
				Severity: "low",
				Evidence: []audit.Evidence{
					{Type: "configuration", Ref: "app-attest", Description: "native attestation"},
				},
				ResolvedDate: "2026-06-02",
			},
		},
	}

	// Default profile (empty string) returns top-level values
	if got := f.StatusForProfile(""); got != "open" {
		t.Errorf("StatusForProfile('') = %q, want 'open'", got)
	}
	if got := f.SeverityForProfile(""); got != "high" {
		t.Errorf("SeverityForProfile('') = %q, want 'high'", got)
	}
	if got := f.EvidenceForProfile(""); len(got) != 1 {
		t.Errorf("EvidenceForProfile('') = %d items, want 1", len(got))
	}

	// native_only profile returns overridden values
	if got := f.StatusForProfile("native_only"); got != "resolved" {
		t.Errorf("StatusForProfile('native_only') = %q, want 'resolved'", got)
	}
	if got := f.SeverityForProfile("native_only"); got != "low" {
		t.Errorf("SeverityForProfile('native_only') = %q, want 'low'", got)
	}
	// Evidence combines base + profile
	ev := f.EvidenceForProfile("native_only")
	if len(ev) != 2 {
		t.Errorf("EvidenceForProfile('native_only') = %d items, want 2", len(ev))
	}

	// Unknown profile falls back to top-level
	if got := f.StatusForProfile("unknown"); got != "open" {
		t.Errorf("StatusForProfile('unknown') = %q, want 'open'", got)
	}

	// Terminal checks
	if f.IsTerminalForProfile("") {
		t.Error("should not be terminal for default profile")
	}
	if !f.IsTerminalForProfile("native_only") {
		t.Error("should be terminal for native_only profile")
	}
	if !f.IsResolvedForProfile("native_only") {
		t.Error("should be resolved for native_only profile")
	}
}

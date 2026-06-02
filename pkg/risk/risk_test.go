package risk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndQuery(t *testing.T) {
	dir := t.TempDir()
	content := `risk_register:
  id: platform
  title: Platform Risk Register
  owner: platform
  last_review: "2026-06-01"
  next_review: "2026-09-01"

risks:
  - id: RSK-P-001
    finding: STR-M-1
    profiles: [full]
    title: "Session token in sessionStorage"
    severity: medium
    residual_severity: low
    status: accepted
    description: "Test risk"
    compensating_controls:
      - "CSP script-src self"
    residual_risk: "Supply-chain only"
    decision:
      date: "2026-06-01"
      rationale: "Mitigated by CSP + passkey"
      reviewer: "leifj"
      review_interval: "quarterly"
  - id: RSK-P-002
    finding: STR-M-2
    profiles: [full, native_only]
    title: "Schema downgrade"
    severity: medium
    residual_severity: low
    status: accepted
    description: "Client-side only"
    compensating_controls:
      - "Encrypted blob"
    residual_risk: "Self-harm only"
    decision:
      date: "2026-06-01"
      rationale: "Low residual"
      reviewer: "leifj"
      review_interval: "quarterly"
`
	if err := os.WriteFile(filepath.Join(dir, "platform.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	set, err := Load(dir, []string{"platform.yaml"})
	if err != nil {
		t.Fatal(err)
	}

	if len(set.RisksByID) != 2 {
		t.Fatalf("expected 2 risks, got %d", len(set.RisksByID))
	}

	// Test RisksByFinding
	if refs := set.RisksByFinding["STR-M-1"]; len(refs) != 1 {
		t.Fatalf("expected 1 risk for STR-M-1, got %d", len(refs))
	}

	// Test AppliesToProfile
	r1 := set.RisksByID["RSK-P-001"].Risk
	if !r1.AppliesToProfile("full") {
		t.Error("RSK-P-001 should apply to full profile")
	}
	if r1.AppliesToProfile("native_only") {
		t.Error("RSK-P-001 should not apply to native_only profile")
	}

	r2 := set.RisksByID["RSK-P-002"].Risk
	if !r2.AppliesToProfile("native_only") {
		t.Error("RSK-P-002 should apply to native_only profile")
	}
	if !r2.AppliesToProfile("full") {
		t.Error("RSK-P-002 should apply to full profile")
	}

	// Empty profile always matches
	if !r1.AppliesToProfile("") {
		t.Error("empty profile should always match")
	}
}

func TestLoadEmpty(t *testing.T) {
	set, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.RisksByID) != 0 {
		t.Fatalf("expected 0 risks, got %d", len(set.RisksByID))
	}
}

func TestDuplicateRiskID(t *testing.T) {
	dir := t.TempDir()
	content := `risk_register:
  id: test
  title: Test
  owner: platform

risks:
  - id: RSK-DUP
    finding: F-1
    title: "Dup1"
    severity: medium
    residual_severity: low
    status: accepted
    decision:
      date: "2026-01-01"
      reviewer: "test"
  - id: RSK-DUP
    finding: F-2
    title: "Dup2"
    severity: medium
    residual_severity: low
    status: accepted
    decision:
      date: "2026-01-01"
      reviewer: "test"
`
	if err := os.WriteFile(filepath.Join(dir, "test.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir, []string{"test.yaml"})
	if err == nil {
		t.Fatal("expected error for duplicate risk ID")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(":::invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir, []string{"bad.yaml"})
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noperm.yaml")
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0000)
	defer os.Chmod(path, 0644)

	_, err := Load(dir, []string{"noperm.yaml"})
	if err == nil {
		t.Fatal("expected error for unreadable file")
	}
}

func TestIsOverdueRegister(t *testing.T) {
	tests := []struct {
		name       string
		nextReview string
		want       bool
	}{
		{"empty", "", false},
		{"future", "2099-12-31", false},
		{"past", "2020-01-01", true},
		{"invalid", "not-a-date", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := RegisterHeader{NextReview: tt.nextReview}
			if got := IsOverdueRegister(reg); got != tt.want {
				t.Errorf("IsOverdueRegister(%q) = %v, want %v", tt.nextReview, got, tt.want)
			}
		})
	}
}

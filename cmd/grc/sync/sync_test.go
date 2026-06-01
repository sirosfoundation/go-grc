package sync

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	ghpkg "github.com/sirosfoundation/go-grc/pkg/github"
	"github.com/spf13/cobra"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func TestParseIssueRef(t *testing.T) {
	tests := []struct {
		input   string
		repo    string
		number  int
		wantErr bool
	}{
		{"org/repo#42", "org/repo", 42, false},
		{"sirosfoundation/compliance#103", "sirosfoundation/compliance", 103, false},
		{"bad-ref", "", 0, true},
		{"no-slash#42", "", 0, true},
		{"org/repo#notanum", "", 0, true},
	}
	for _, tt := range tests {
		ref, err := parseIssueRef(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseIssueRef(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseIssueRef(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if ref.Repo != tt.repo || ref.Number != tt.number {
			t.Errorf("parseIssueRef(%q) = %s#%d, want %s#%d", tt.input, ref.Repo, ref.Number, tt.repo, tt.number)
		}
	}
}

func TestOwnerFromLabels(t *testing.T) {
	tests := []struct {
		labels []string
		want   string
	}{
		{[]string{"compliance", "owner:platform"}, "platform"},
		{[]string{"owner:operator", "owner:platform"}, "operator, platform"},
		{[]string{"compliance", "severity:high"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		got := ownerFromLabels(tt.labels)
		if got != tt.want {
			t.Errorf("ownerFromLabels(%v) = %q, want %q", tt.labels, got, tt.want)
		}
	}
}

func TestHasLabel(t *testing.T) {
	labels := []string{"compliance", "severity:high", "in-progress"}
	if !hasLabel(labels, "compliance") {
		t.Error("expected hasLabel to find 'compliance'")
	}
	if hasLabel(labels, "missing") {
		t.Error("expected hasLabel to not find 'missing'")
	}
	if hasLabel(nil, "anything") {
		t.Error("expected hasLabel on nil to return false")
	}
}

func TestHasIssueRef(t *testing.T) {
	refs := []audit.IssueRef{
		{Repo: "org/repo", Number: 1},
		{Repo: "org/other", Number: 2},
	}
	if !hasIssueRef(refs, ghpkg.IssueRef{Repo: "org/repo", Number: 1}) {
		t.Error("expected to find org/repo#1")
	}
	if hasIssueRef(refs, ghpkg.IssueRef{Repo: "org/repo", Number: 99}) {
		t.Error("expected not to find org/repo#99")
	}
	if hasIssueRef(nil, ghpkg.IssueRef{Repo: "org/repo", Number: 1}) {
		t.Error("expected nil refs to return false")
	}
}

func TestHasEvidenceRef(t *testing.T) {
	evidence := []audit.Evidence{
		{Type: "merged_pr", Ref: "org/repo#10"},
		{Type: "closed_issue", Ref: "org/repo#20"},
	}
	if !hasEvidenceRef(evidence, "org/repo", 10) {
		t.Error("expected to find org/repo#10")
	}
	if hasEvidenceRef(evidence, "org/repo", 99) {
		t.Error("expected not to find org/repo#99")
	}
}

func TestBuildIssueBody(t *testing.T) {
	f := &audit.Finding{
		ID:          "TEST-001",
		Title:       "Test finding",
		Severity:    "high",
		Owner:       "platform",
		Description: "Some description",
		Controls:    []string{"SID-AUTH-01", "SID-KEY-03"},
	}
	a := &audit.Audit{
		ID:    "SA-2025-001",
		Title: "Test Audit",
	}

	body := buildIssueBody(f, a)

	if body == "" {
		t.Fatal("buildIssueBody returned empty string")
	}
	// Check key elements are present
	for _, want := range []string{
		"TEST-001", "Test Audit", "high", "platform",
		"Some description", "SID-AUTH-01", "SID-KEY-03",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("buildIssueBody missing %q", want)
		}
	}
}

func TestSyncCommand_NoToken(t *testing.T) {
	root := testdataDir()

	// Ensure no GitHub token is set
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	cmd := NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)
	parent.SilenceErrors = true
	parent.SilenceUsage = true

	parent.SetArgs([]string{"sync"})
	if err := parent.Execute(); err == nil {
		t.Fatal("expected error when GITHUB_TOKEN not set")
	}
}

func TestSyncLinkCommand(t *testing.T) {
	// Copy testdata to temp dir since link modifies files
	tmp := t.TempDir()
	copyTestdata(t, testdataDir(), tmp)

	cmd := NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmp, "root")
	parent.AddCommand(cmd)

	// Link an issue to finding SA-001 (which exists in testdata)
	parent.SetArgs([]string{"sync", "link", "F-001", "org/repo#42"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("sync link failed: %v", err)
	}

	// Verify the finding now has the issue ref
	audits, err := audit.Load(filepath.Join(tmp, "audits"))
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := audits.FindingsByID["F-001"]
	if !ok {
		t.Fatal("F-001 not found after link")
	}
	found := false
	for _, ir := range ref.Finding.Issues {
		if ir.Repo == "org/repo" && ir.Number == 42 {
			found = true
		}
	}
	if !found {
		t.Error("expected org/repo#42 in F-001 issues")
	}
}

func TestSyncLinkCommand_PR(t *testing.T) {
	tmp := t.TempDir()
	copyTestdata(t, testdataDir(), tmp)

	cmd := NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmp, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"sync", "link", "--pr", "F-001", "org/repo#99"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("sync link --pr failed: %v", err)
	}

	audits, err := audit.Load(filepath.Join(tmp, "audits"))
	if err != nil {
		t.Fatal(err)
	}
	ref := audits.FindingsByID["F-001"]
	found := false
	for _, pr := range ref.Finding.PullRequests {
		if pr.Repo == "org/repo" && pr.Number == 99 {
			found = true
		}
	}
	if !found {
		t.Error("expected org/repo#99 in F-001 pull_requests")
	}
}

func TestSyncLinkCommand_NotFound(t *testing.T) {
	tmp := t.TempDir()
	copyTestdata(t, testdataDir(), tmp)

	cmd := NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmp, "root")
	parent.AddCommand(cmd)
	parent.SilenceErrors = true
	parent.SilenceUsage = true

	parent.SetArgs([]string{"sync", "link", "NONEXISTENT", "org/repo#42"})
	if err := parent.Execute(); err == nil {
		t.Fatal("expected error for nonexistent finding")
	}
}

// copyTestdata copies the testdata directory to dst for mutation-safe tests.
func copyTestdata(t *testing.T, src, dst string) {
	t.Helper()
	entries := []struct{ dir, glob string }{
		{"", ".grc.yaml"},
		{"catalog/technical", "*.yaml"},
		{"catalog/organizational", "*.yaml"},
		{"catalog/frameworks", "*.yaml"},
		{"mappings", "*.yaml"},
		{"audits", "*.yaml"},
		{"site", "docusaurus.config.ts"},
	}
	for _, e := range entries {
		srcDir := filepath.Join(src, e.dir)
		dstDir := filepath.Join(dst, e.dir)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			t.Fatal(err)
		}
		matches, _ := filepath.Glob(filepath.Join(srcDir, e.glob))
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dstDir, filepath.Base(m)), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

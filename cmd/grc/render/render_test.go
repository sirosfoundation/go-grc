package render_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/render"
	"github.com/sirosfoundation/go-grc/internal/testutil"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func TestRenderCommand(t *testing.T) {
	root := testdataDir()
	// Render writes to <root>/site/docs — use a temp copy to avoid polluting testdata
	tmpDir := t.TempDir()

	// Copy testdata to temp dir
	if err := copyDir(root, tmpDir); err != nil {
		t.Fatalf("copying testdata: %v", err)
	}
	// Render expects site/docs/ directory to exist
	if err := os.MkdirAll(filepath.Join(tmpDir, "site", "docs"), 0755); err != nil {
		t.Fatalf("creating site/docs dir: %v", err)
	}

	cmd := render.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmpDir, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"render", "--profile", "public"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("render --profile public failed: %v", err)
	}

	// Verify some output was generated
	docsDir := filepath.Join(tmpDir, "site", "docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatalf("reading rendered docs dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("render produced no output files")
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func TestRenderCommand_Private(t *testing.T) {
	root := testdataDir()
	tmpDir := t.TempDir()

	if err := copyDir(root, tmpDir); err != nil {
		t.Fatalf("copying testdata: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "site", "docs"), 0755); err != nil {
		t.Fatalf("creating site/docs dir: %v", err)
	}

	cmd := render.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmpDir, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"render", "--profile", "private"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("render --profile private failed: %v", err)
	}

	// Verify risk register was rendered
	riskDir := filepath.Join(tmpDir, "site", "docs", "risk-register")
	entries, err := os.ReadDir(riskDir)
	if err != nil {
		t.Fatalf("reading risk-register dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("render produced no risk register output files")
	}

	page, err := os.ReadFile(filepath.Join(riskDir, "rsk_p_001.md"))
	if err != nil {
		t.Fatalf("reading risk page: %v", err)
	}
	if !strings.Contains(string(page), "| **Risk Owner** | Test Owner |") {
		t.Errorf("risk page does not show the risk owner:\n%s", page)
	}
	for _, want := range []string{"## Treatment action", "| **Responsible** | Test Owner |", "| **Treatment Status** | in_progress |", "| **Due Date** | 2026-12-01 |"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("risk page lacks %q:\n%s", want, page)
		}
	}
	index, err := os.ReadFile(filepath.Join(riskDir, "index.md"))
	if err != nil {
		t.Fatalf("reading risk index: %v", err)
	}
	if !strings.Contains(string(index), "| Test Owner |") {
		t.Errorf("risk index does not show the risk owner:\n%s", index)
	}
	if !strings.Contains(string(index), "| Treatment |") || !strings.Contains(string(index), "| in_progress |") {
		t.Errorf("risk index lacks the treatment column:\n%s", index)
	}
}

func TestRenderCommand_RiskWithoutTreatmentAction(t *testing.T) {
	tmpDir := testutil.CopyFixtureWithoutTreatment(t, testdataDir())
	if err := os.MkdirAll(filepath.Join(tmpDir, "site", "docs"), 0755); err != nil {
		t.Fatal(err)
	}

	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmpDir, "root")
	parent.AddCommand(render.NewCommand())
	parent.SetArgs([]string{"render", "--profile", "private"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("render failed: %v", err)
	}

	riskDir := filepath.Join(tmpDir, "site", "docs", "risk-register")
	page, err := os.ReadFile(filepath.Join(riskDir, "rsk_p_001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "No treatment action recorded") {
		t.Errorf("risk page does not flag the missing treatment action:\n%s", page)
	}
	index, err := os.ReadFile(filepath.Join(riskDir, "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "| none |") {
		t.Errorf("risk index does not show treatment none:\n%s", index)
	}
}

func renderFixture(t *testing.T, profile, soaStatus string) string {
	t.Helper()
	tmpDir := t.TempDir()
	if err := copyDir(testdataDir(), tmpDir); err != nil {
		t.Fatal(err)
	}
	renderInto(t, tmpDir, profile, soaStatus)
	return tmpDir
}

func renderInto(t *testing.T, tmpDir, profile, soaStatus string) {
	t.Helper()
	write := func(rel, body string) {
		p := filepath.Join(tmpDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("site/docs/.keep", "")
	write("audits/iso-2026/audit.yaml", "audit:\n  id: REAL\nfindings:\n  - id: AUD-REAL-1\n    title: Real audit finding\n    severity: low\n    status: open\n    owner: o\n    controls: []\n")
	write("audits/iso-2026/soa.yaml", `soa:
  id: SOA-TEST
  title: Test Statement of Applicability
  version: "1.0"
`+soaStatus+`
  date: "2026-10-01"
requirements:
- annex_a: A.5.1
  title: Policies for information security
  applicable: true
  status: implemented
  justification: Policy approved | by the Board
  evidence: [policies/secret-internal-evidence.md]
`)
	write("assessments/self.yaml", "audit:\n  id: SELF\n  title: SECRET-ASSESSMENT-TITLE\nfindings:\n  - id: ASM-SECRET-1\n    title: SECRET-ASSESSMENT-FINDING\n    severity: high\n    status: open\n    owner: o\n    controls: []\n    description: SECRET-ASSESSMENT-BODY\n    tracking_issue: {repo: org/repo, number: 1}\n")

	cmd := render.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmpDir, "root")
	parent.AddCommand(cmd)
	parent.SetArgs([]string{"render", "--profile", profile})
	if err := parent.Execute(); err != nil {
		t.Fatalf("render --profile %s: %v", profile, err)
	}
}

const approved = "  status: approved\n  approved_date: \"2026-10-20\""

func readAll(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		files[p] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestRenderPublicShowsSoAButNeverAssessments(t *testing.T) {
	root := renderFixture(t, "public", approved)

	page, err := os.ReadFile(filepath.Join(root, "site", "docs", "audits", "iso-2026.md"))
	if err != nil {
		t.Fatalf("public site has no SoA page: %v", err)
	}
	for _, want := range []string{"| A.5.1 |", "Policies for information security", "Policy approved \\| by the Board", "| Justification |"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("SoA page missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(string(page), "Evidence") || strings.Contains(string(page), "secret-internal-evidence") {
		t.Errorf("public SoA page exposes the evidence column:\n%s", page)
	}

	for p, content := range readAll(t, filepath.Join(root, "site")) {
		for _, marker := range []string{"SECRET-ASSESSMENT", "ASM-SECRET-1", "AUD-REAL-1"} {
			if strings.Contains(content, marker) {
				t.Errorf("public output %s contains %q", p, marker)
			}
		}
	}
	sidebars, _ := os.ReadFile(filepath.Join(root, "site", "sidebars.ts"))
	if !strings.Contains(string(sidebars), "auditsSidebar") {
		t.Error("public sidebars.ts has no auditsSidebar")
	}
}

func TestRenderPrivateAssessmentsStillFeedFindings(t *testing.T) {
	root := renderFixture(t, "private", "  status: in_review")
	page, err := os.ReadFile(filepath.Join(root, "site", "docs", "audits", "iso-2026.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "| Evidence |") || !strings.Contains(string(page), "secret-internal-evidence") {
		t.Errorf("private SoA page lacks the evidence column:\n%s", page)
	}
	found := false
	for p, content := range readAll(t, filepath.Join(root, "site", "docs", "findings")) {
		if strings.Contains(content, "ASM-SECRET-1") {
			found = true
			_ = p
		}
	}
	if !found {
		t.Error("private findings pages lost the internal assessment's finding")
	}
}

func TestRenderPublicHidesUnapprovedSoA(t *testing.T) {
	for name, status := range map[string]string{
		"draft":     "  status: draft",
		"in_review": "  status: in_review",
		"absent":    "",
		"unknown":   "  status: published",
	} {
		t.Run(name, func(t *testing.T) {
			root := renderFixture(t, "public", status)
			if _, err := os.Stat(filepath.Join(root, "site", "docs", "audits")); err == nil {
				t.Error("public site has an audits directory for an unapproved statement")
			}
			for p, content := range readAll(t, filepath.Join(root, "site")) {
				for _, marker := range []string{"SOA-TEST", "Test Statement of Applicability", "Policies for information security", "auditsSidebar"} {
					if strings.Contains(content, marker) {
						t.Errorf("public output %s contains %q", p, marker)
					}
				}
			}
		})
	}
}

func TestRenderPrivateShowsDraftSoAWithBanner(t *testing.T) {
	root := renderFixture(t, "private", "  status: draft")
	page, err := os.ReadFile(filepath.Join(root, "site", "docs", "audits", "iso-2026.md"))
	if err != nil {
		t.Fatalf("private site lost the draft SoA: %v", err)
	}
	for _, want := range []string{":::caution Draft", "| **Status** | Draft |"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("draft SoA page missing %q:\n%s", want, page)
		}
	}
}

// A statement that goes back to draft must not stay online from an earlier
// public render.
func TestRenderPublicRemovesSoAWhenNoLongerApproved(t *testing.T) {
	root := renderFixture(t, "public", approved)
	if _, err := os.Stat(filepath.Join(root, "site", "docs", "audits", "iso-2026.md")); err != nil {
		t.Fatalf("approved SoA not published: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "audits", "iso-2026", "soa.yaml"), []byte("soa:\n  id: SOA-TEST\n  status: draft\nrequirements: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	renderInto(t, root, "public", "  status: draft") // rewrites the fixture as a draft
	if _, err := os.Stat(filepath.Join(root, "site", "docs", "audits")); err == nil {
		t.Error("stale published SoA pages survived the SoA going back to draft")
	}
}

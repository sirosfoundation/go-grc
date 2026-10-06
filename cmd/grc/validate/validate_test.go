package validate_test

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/validate"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func TestValidateCommand_Passes(t *testing.T) {
	root := testdataDir()

	cmd := validate.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"validate"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("validate failed on valid testdata: %v", err)
	}
}

func TestValidateCommand_LintAlias(t *testing.T) {
	root := testdataDir()

	cmd := validate.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"lint"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("lint alias failed: %v", err)
	}
}

// brokenFixture copies the valid testdata and injects one instance of every
// kind of problem the validator knows about.
func brokenFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(testdataDir())); err != nil {
		t.Fatal(err)
	}
	appendTo := func(rel, extra string) {
		p := filepath.Join(root, rel)
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(extra); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	appendTo("catalog/organizational/governance.yaml", `  - id: "BAD-01"
    title: "Broken control"
    category: "bogus"
    csf_function: "bogus"
    status: "weird"
`)
	appendTo("mappings/eudi-secreq.yaml", `  - id: "WTE_07"
    controls:
      - "NOPE-1"
    result: "not_assessed"
`)
	appendTo("audits/sa-2025-001.yaml", `  - id: "F-900"
    title: "Broken finding"
    severity: "extreme"
    status: "weird"
    controls:
      - "NOPE-2"
    evidence:
      - {}
      - ref: "only-a-ref"
      - type: "bogus"
        ref: "y"
    profiles:
      ghost:
        status: "resolved"
  - id: "F-901"
    title: "Resolved without evidence"
    severity: "low"
    status: "resolved"
    controls:
      - "GOV-POL-01"
  - id: "F-902"
    title: "Accepted without risk"
    severity: "low"
    status: "accepted"
    controls:
      - "GOV-POL-01"
`)
	riskYAML := `risk_register:
  id: platform
  title: Platform Risk Register
  owner: platform
  last_review: "2020-01-01"
  next_review: "2020-06-01"
risks:
  - id: RSK-BAD
    finding: F-NOPE
    profiles: [ghost]
    title: "Broken risk"
    status: bogus
    description: "d"
`
	if err := os.WriteFile(filepath.Join(root, "risk-register", "platform.yaml"), []byte(riskYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(root, ".grc.yaml")
	b, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Replace(string(b), "  files:\n    - platform.yaml\n", "  files:\n    - platform.yaml\n  methodology: policies/missing.md\n", 1)
	s = strings.Replace(s, "oscal:\n", "  - id: ghost\n    name: Ghost\n    catalog_file: ghost.yaml\n    mapping_file: ghost.yaml\n    key_field: id\n\noscal:\n", 1)
	if err := os.WriteFile(cfg, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// runValidate executes the validate command against root, capturing stdout.
func runValidate(t *testing.T, root string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	parent := &cobra.Command{Use: "grc", SilenceUsage: true, SilenceErrors: true}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(validate.NewCommand())
	parent.SetArgs([]string{"validate"})
	execErr := parent.Execute()

	os.Stdout = old
	_ = w.Close()
	return <-done, execErr
}

func TestValidateReportsEveryProblemKind(t *testing.T) {
	out, err := runValidate(t, brokenFixture(t))
	if err == nil {
		t.Fatalf("validation of a broken fixture must fail:\n%s", out)
	}
	for _, want := range []string{
		`control BAD-01: invalid category "bogus"`,
		`control BAD-01: invalid csf_function "bogus"`,
		`control BAD-01: invalid status "weird"`,
		`framework eudi: duplicate requirement key "WTE_07"`,
		`framework eudi, entry WTE_07: unknown control "NOPE-1"`,
		`framework ghost: mapping file ghost.yaml not found`,
		`finding F-900: invalid status "weird"`,
		`finding F-900: invalid severity "extreme"`,
		`finding F-900: unknown control "NOPE-2"`,
		`finding F-900: evidence[0] has all empty fields`,
		`finding F-900: evidence[1] missing type`,
		`finding F-900: evidence[2] unknown type "bogus"`,
		`finding F-900: profile override references unknown profile "ghost"`,
		`finding F-902: status is 'accepted' but no risk register entry exists`,
		`risk register: methodology "policies/missing.md" not usable`,
		`risk RSK-BAD: invalid status "bogus"`,
		`risk RSK-BAD: references unknown finding "F-NOPE"`,
		`risk RSK-BAD: missing risk owner`,
		`risk RSK-BAD: missing decision date`,
		`risk RSK-BAD: missing decision reviewer`,
		`risk RSK-BAD: references unknown profile "ghost"`,
		`finding F-901: resolved without evidence`,
		`control BAD-01: orphan`,
		`risk register platform: review overdue (next_review: 2020-06-01)`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if !strings.Contains(out, "Validation found") || strings.Contains(out, "Validation passed") {
		t.Errorf("unexpected summary:\n%s", out)
	}
	if !strings.Contains(err.Error(), "validation error(s)") {
		t.Errorf("err = %v", err)
	}
}

func TestValidatePassingOutputAndLoadErrors(t *testing.T) {
	out, err := runValidate(t, testdataDir())
	if err != nil || !strings.Contains(out, "Validation passed.") {
		t.Errorf("valid data: %v %q", err, out)
	}

	root := brokenFixture(t)
	if err := os.WriteFile(filepath.Join(root, "audits", "sa-2025-001.yaml"), []byte("audit: [bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runValidate(t, root); err == nil || !strings.Contains(err.Error(), "loading audits") {
		t.Errorf("audits err = %v", err)
	}

	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".grc.yaml"), []byte("project: [bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runValidate(t, root); err == nil {
		t.Error("bad config must fail")
	}
}

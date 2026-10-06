package risk_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	riskcmd "github.com/sirosfoundation/go-grc/cmd/grc/risk"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func runRisk(t *testing.T, args ...string) string {
	t.Helper()
	root := testdataDir()
	cmd := riskcmd.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	parent.SetArgs(append([]string{"risk"}, args...))
	err := parent.Execute()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("risk %v failed: %v", args, err)
	}
	return buf.String()
}

func TestListCommand_Text(t *testing.T) {
	out := runRisk(t, "list")
	if len(out) == 0 {
		t.Error("expected output from risk list")
	}
}

func TestListCommand_JSON(t *testing.T) {
	out := runRisk(t, "list", "--format", "json")
	var entries []riskcmd.RiskEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ID != "RSK-P-001" {
		t.Errorf("expected RSK-P-001, got %s", entries[0].ID)
	}
}

func TestListCommand_RegisterOwnerFilter(t *testing.T) {
	out := runRisk(t, "list", "--format", "json", "--register-owner", "operator")
	var entries []riskcmd.RiskEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for operator, got %d", len(entries))
	}
}

func TestListCommand_RiskOwner(t *testing.T) {
	out := runRisk(t, "list", "--format", "json", "--owner", "test owner")
	var entries []riskcmd.RiskEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(entries) != 1 || entries[0].Owner != "Test Owner" || entries[0].RegisterOwner != "platform" {
		t.Errorf("unexpected entries for risk owner filter: %+v", entries)
	}

	out = runRisk(t, "list", "--format", "json", "--owner", "someone else")
	entries = nil
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for other owner, got %d", len(entries))
	}
}

func TestValidateCommand_MissingOwner(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(testdataDir())); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "risk-register", "platform.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stripped := strings.Replace(string(data), "    owner: \"Test Owner\"\n", "", 1)
	if stripped == string(data) {
		t.Fatal("fixture has no risk owner to remove")
	}
	if err := os.WriteFile(path, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := riskcmd.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)
	parent.SetArgs([]string{"risk", "validate"})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err = parent.Execute()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err == nil {
		t.Fatal("expected validation to fail for a risk without an owner")
	}
	if !strings.Contains(buf.String(), "missing risk owner") {
		t.Errorf("expected 'missing risk owner' problem, got:\n%s", buf.String())
	}
}

func TestListCommand_ProfileFilter(t *testing.T) {
	out := runRisk(t, "list", "--format", "json", "--profile", "native_only")
	var entries []riskcmd.RiskEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// RSK-P-001 has profiles: [full], so native_only should not match
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for native_only, got %d", len(entries))
	}
}

func TestValidateCommand(t *testing.T) {
	out := runRisk(t, "validate")
	if len(out) == 0 {
		t.Error("expected output from risk validate")
	}
}

func TestSummaryCommand_Text(t *testing.T) {
	out := runRisk(t, "summary")
	if len(out) == 0 {
		t.Error("expected output from risk summary")
	}
}

func TestSummaryCommand_JSON(t *testing.T) {
	out := runRisk(t, "summary", "--format", "json")
	var summary riskcmd.RiskSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if summary.Total != 1 {
		t.Errorf("expected 1 total, got %d", summary.Total)
	}
	if summary.ByOwner["Test Owner"] != 1 || summary.Unowned != 0 {
		t.Errorf("unexpected owner breakdown: %+v", summary)
	}
	if summary.ByRegisterOwner["platform"] != 1 {
		t.Errorf("unexpected register owner breakdown: %+v", summary)
	}
}

func TestValidateCommand_MissingTreatmentAction(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(testdataDir())); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "risk-register", "platform.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(data), "    treatment_action:")
	j := strings.Index(string(data), "    tracking:")
	if i < 0 || j < i {
		t.Fatal("fixture has no treatment_action to remove")
	}
	stripped := string(data)[:i] + string(data)[j:]
	if err := os.WriteFile(path, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}

	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(riskcmd.NewCommand())
	parent.SetArgs([]string{"risk", "validate"})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err = parent.Execute()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err == nil {
		t.Fatal("expected validation to fail for a non-draft risk without treatment_action")
	}
	if !strings.Contains(buf.String(), "missing treatment_action") {
		t.Errorf("expected 'missing treatment_action' problem, got:\n%s", buf.String())
	}
}

func TestListCommand_TreatmentAction(t *testing.T) {
	out := runRisk(t, "list", "--format", "json")
	var entries []riskcmd.RiskEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(entries) != 1 || entries[0].TreatmentStatus != "in_progress" || entries[0].TreatmentAction == nil || entries[0].TreatmentAction.Responsible != "Test Owner" {
		t.Errorf("treatment action not listed: %+v", entries)
	}
	if !strings.Contains(out, `"treatment_status": "in_progress"`) || !strings.Contains(out, `"due_date": "2026-12-01"`) {
		t.Errorf("JSON lacks treatment fields:\n%s", out)
	}
}

func TestSummaryCommand_TreatmentStatus(t *testing.T) {
	out := runRisk(t, "summary", "--format", "json")
	var summary riskcmd.RiskSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.ByTreatmentStatus["in_progress"] != 1 {
		t.Errorf("unexpected treatment breakdown: %+v", summary.ByTreatmentStatus)
	}
	if text := runRisk(t, "summary"); !strings.Contains(text, "in_progress") {
		t.Errorf("text summary lacks treatment status:\n%s", text)
	}
}

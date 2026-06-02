package risk_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	riskcmd "github.com/sirosfoundation/go-grc/cmd/grc/risk"
	"github.com/spf13/cobra"
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

func TestListCommand_OwnerFilter(t *testing.T) {
	out := runRisk(t, "list", "--format", "json", "--owner", "operator")
	var entries []riskcmd.RiskEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for operator, got %d", len(entries))
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
}

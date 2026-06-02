package derive_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/derive"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func runDerive(t *testing.T, args ...string) {
	t.Helper()
	root := testdataDir()
	cmd := derive.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)
	parent.SetArgs(append([]string{"derive"}, args...))
	if err := parent.Execute(); err != nil {
		t.Fatalf("derive %v failed: %v", args, err)
	}
}

func TestDeriveCommand_DryRun(t *testing.T) {
	runDerive(t, "--dry-run")
}

func TestDeriveCommand_JSON(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runDerive(t, "--dry-run", "--format", "json")

	w.Close()
	os.Stdout = old

	var buf strings.Builder
	data, _ := io.ReadAll(r)
	buf.Write(data)

	var report derive.DeriveReport
	if err := json.Unmarshal([]byte(buf.String()), &report); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if !report.DryRun {
		t.Error("expected dry_run=true in JSON output")
	}
	if report.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestDeriveCommand_Changelog(t *testing.T) {
	changelogPath := filepath.Join(t.TempDir(), "CHANGELOG.md")
	runDerive(t, "--dry-run", "--changelog", changelogPath)

	// --dry-run should NOT write a changelog
	if _, err := os.Stat(changelogPath); !os.IsNotExist(err) {
		t.Fatal("changelog should not be created in dry-run mode")
	}
}

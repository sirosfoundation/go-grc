package derive_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirosfoundation/go-grc/cmd/grc/derive"
	"github.com/spf13/cobra"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func TestDeriveCommand_DryRun(t *testing.T) {
	root := testdataDir()

	cmd := derive.NewCommand()
	// Propagate root flag like the real CLI does
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"derive", "--dry-run"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("derive --dry-run failed: %v", err)
	}
}

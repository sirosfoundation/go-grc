package status_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirosfoundation/go-grc/cmd/grc/status"
	"github.com/spf13/cobra"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func TestStatusCommand(t *testing.T) {
	root := testdataDir()

	cmd := status.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"status"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("status failed: %v", err)
	}
}

func TestStatusCommand_JSON(t *testing.T) {
	root := testdataDir()

	cmd := status.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"status", "--format", "json"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("status --format json failed: %v", err)
	}
}

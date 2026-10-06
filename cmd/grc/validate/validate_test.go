package validate_test

import (
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
	if err := os.WriteFile(path, []byte(string(data)[:i]+string(data)[j:]), 0o644); err != nil {
		t.Fatal(err)
	}

	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(validate.NewCommand())
	parent.SetArgs([]string{"validate"})
	if err := parent.Execute(); err == nil {
		t.Fatal("expected grc validate to fail for a non-draft risk without treatment_action")
	}
}

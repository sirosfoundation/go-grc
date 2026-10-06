package validate_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/validate"
	"github.com/sirosfoundation/go-grc/internal/testutil"
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
	root := testutil.CopyFixtureWithoutTreatment(t, testdataDir())

	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(validate.NewCommand())
	parent.SetArgs([]string{"validate"})
	if err := parent.Execute(); err == nil {
		t.Fatal("expected grc validate to fail for a non-draft risk without treatment_action")
	}
}

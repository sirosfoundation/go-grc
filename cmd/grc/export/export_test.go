package export_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirosfoundation/go-grc/cmd/grc/export"
	"github.com/spf13/cobra"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
}

func TestExportCommand(t *testing.T) {
	root := testdataDir()
	outDir := t.TempDir()

	cmd := export.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", root, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"export", "-o", outDir})
	if err := parent.Execute(); err != nil {
		t.Fatalf("export failed: %v", err)
	}

	outPath := filepath.Join(outDir, "evidence-package.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	var pkg map[string]interface{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if _, ok := pkg["controls"]; !ok {
		t.Error("export missing 'controls' key")
	}
	if _, ok := pkg["findings"]; !ok {
		t.Error("export missing 'findings' key")
	}
	if _, ok := pkg["frameworks"]; !ok {
		t.Error("export missing 'frameworks' key")
	}
}

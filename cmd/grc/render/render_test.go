package render_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/render"
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
}

package initialize_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/initialize"
)

func TestInitCommand(t *testing.T) {
	tmp := t.TempDir()

	cmd := initialize.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmp, "root")
	parent.AddCommand(cmd)

	parent.SetArgs([]string{"init", "--name", "Test Project", "--repo", "org/repo"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify .grc.yaml was created
	if _, err := os.Stat(filepath.Join(tmp, ".grc.yaml")); err != nil {
		t.Fatalf(".grc.yaml not created: %v", err)
	}

	// Verify directories
	for _, dir := range []string{
		"catalog/technical",
		"catalog/organizational",
		"catalog/frameworks",
		"mappings",
		"audits",
	} {
		path := filepath.Join(tmp, dir)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("directory %s not created: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}

	// Verify starter control group
	starterPath := filepath.Join(tmp, "catalog", "technical", "security.yaml")
	if _, err := os.Stat(starterPath); err != nil {
		t.Fatalf("starter control group not created: %v", err)
	}
}

func TestInitCommand_AlreadyExists(t *testing.T) {
	tmp := t.TempDir()

	// Create .grc.yaml first
	if err := os.WriteFile(filepath.Join(tmp, ".grc.yaml"), []byte("project: {}"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := initialize.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmp, "root")
	parent.AddCommand(cmd)
	parent.SilenceErrors = true
	parent.SilenceUsage = true

	parent.SetArgs([]string{"init"})
	if err := parent.Execute(); err == nil {
		t.Fatal("expected error when .grc.yaml already exists")
	}
}

func TestInitCommand_Defaults(t *testing.T) {
	tmp := t.TempDir()

	cmd := initialize.NewCommand()
	parent := &cobra.Command{Use: "grc"}
	parent.PersistentFlags().String("root", tmp, "root")
	parent.AddCommand(cmd)

	// Run without --name or --repo to use defaults
	parent.SetArgs([]string{"init"})
	if err := parent.Execute(); err != nil {
		t.Fatalf("init with defaults failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".grc.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if len(content) == 0 {
		t.Fatal(".grc.yaml is empty")
	}
}

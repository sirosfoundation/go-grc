package initialize

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"gopkg.in/yaml.v3"

	"github.com/sirosfoundation/go-grc/pkg/config"
)

func NewCommand() *cobra.Command {
	var (
		name string
		repo string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new go-grc compliance project",
		Long: `Create the directory structure and configuration file for a new
go-grc compliance project. Creates catalog, mappings, audits directories
and a .grc.yaml configuration file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, name, repo)
		},
	}
	cmd.Flags().StringVar(&name, "name", "Compliance Dashboard", "Project display name")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repo (owner/name) for issue tracking")
	return cmd
}

func run(root, name, repo string) error {
	configPath := filepath.Join(root, ".grc.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf(".grc.yaml already exists in %s", root)
	}

	// Create directories
	dirs := []string{
		filepath.Join(root, "catalog", "technical"),
		filepath.Join(root, "catalog", "organizational"),
		filepath.Join(root, "catalog", "frameworks"),
		filepath.Join(root, "mappings"),
		filepath.Join(root, "audits"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
		fmt.Printf("  created %s/\n", d)
	}

	// Write .grc.yaml
	grc := config.GRCFile{
		Project: config.ProjectConfig{
			Name: name,
			Repo: repo,
		},
		Catalog: config.CatalogConfig{
			Dir:           "catalog",
			Subdirs:       []string{"technical", "organizational"},
			FrameworksDir: "frameworks",
		},
		Mappings: config.DirConfig{Dir: "mappings"},
		Audits:   config.DirConfig{Dir: "audits"},
		Site:     config.DirConfig{Dir: "site/docs"},
		OSCAL:    config.DirConfig{Dir: "oscal"},
	}

	data, err := yaml.Marshal(&grc)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("writing .grc.yaml: %w", err)
	}
	fmt.Printf("  created .grc.yaml\n")

	// Write a starter control group
	starterGroup := `group:
  id: security
  title: Security Controls

controls: []
`
	starterPath := filepath.Join(root, "catalog", "technical", "security.yaml")
	if err := os.WriteFile(starterPath, []byte(starterGroup), 0644); err != nil {
		return fmt.Errorf("writing starter group: %w", err)
	}
	fmt.Printf("  created catalog/technical/security.yaml\n")

	fmt.Printf("\nProject initialized in %s\n", root)
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit .grc.yaml to set your project name and repo")
	fmt.Println("  2. Add controls to catalog/technical/*.yaml")
	fmt.Println("  3. Add framework mappings to mappings/")
	fmt.Println("  4. Run: grc render")
	return nil
}

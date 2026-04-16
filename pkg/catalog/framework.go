package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FrameworkRequirement holds the normative text for one framework requirement.
type FrameworkRequirement struct {
	ID          string `yaml:"id"`
	Title       string `yaml:"title"`
	Section     string `yaml:"section"`
	Description string `yaml:"description"`
}

// FrameworkCatalog holds the normative requirement text for a framework.
type FrameworkCatalog struct {
	Framework struct {
		ID      string `yaml:"id"`
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
		Source  string `yaml:"source"`
	} `yaml:"framework"`
	Requirements []FrameworkRequirement           `yaml:"requirements"`
	ByID         map[string]*FrameworkRequirement `yaml:"-"`
}

// LoadFrameworkCatalog reads a framework catalog YAML (e.g. catalog/frameworks/eudi-secreq.yaml).
func LoadFrameworkCatalog(catalogDir, name string) (*FrameworkCatalog, error) {
	path := filepath.Join(catalogDir, "frameworks", name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	var fc FrameworkCatalog
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}

	fc.ByID = make(map[string]*FrameworkRequirement, len(fc.Requirements))
	for i := range fc.Requirements {
		fc.ByID[fc.Requirements[i].ID] = &fc.Requirements[i]
	}

	return &fc, nil
}

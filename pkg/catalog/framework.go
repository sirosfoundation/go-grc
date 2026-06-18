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
// If sections is non-empty, requirements are loaded from those YAML keys instead of "requirements".
func LoadFrameworkCatalog(catalogDir, name string, sections []string) (*FrameworkCatalog, error) {
	path := filepath.Join(catalogDir, "frameworks", name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	if len(sections) > 0 {
		return loadMultiSectionCatalog(data, name, sections)
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

// loadMultiSectionCatalog loads requirements from multiple named sections.
func loadMultiSectionCatalog(data []byte, name string, sections []string) (*FrameworkCatalog, error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}

	// Parse framework metadata
	fc := &FrameworkCatalog{}
	if fwRaw, ok := raw["framework"]; ok {
		fwBytes, _ := yaml.Marshal(fwRaw)
		yaml.Unmarshal(fwBytes, &fc.Framework)
	}

	// Collect requirements from all sections
	for _, section := range sections {
		rawList, ok := raw[section]
		if !ok {
			continue
		}
		// Re-marshal and unmarshal each section as []FrameworkRequirement
		listBytes, err := yaml.Marshal(rawList)
		if err != nil {
			return nil, fmt.Errorf("marshaling section %q in %s: %w", section, name, err)
		}
		var reqs []FrameworkRequirement
		if err := yaml.Unmarshal(listBytes, &reqs); err != nil {
			return nil, fmt.Errorf("parsing section %q in %s: %w", section, name, err)
		}
		fc.Requirements = append(fc.Requirements, reqs...)
	}

	fc.ByID = make(map[string]*FrameworkRequirement, len(fc.Requirements))
	for i := range fc.Requirements {
		fc.ByID[fc.Requirements[i].ID] = &fc.Requirements[i]
	}

	return fc, nil
}

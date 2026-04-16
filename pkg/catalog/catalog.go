// Package catalog provides types and loading for the SirosID control catalog.
//
// The catalog is the authoritative set of controls the platform implements.
// Controls are organized into groups (technical, organizational) defined in
// YAML files under catalog/.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Control represents a single SirosID control.
type Control struct {
	ID                     string   `yaml:"id"`
	Title                  string   `yaml:"title"`
	Description            string   `yaml:"description"`
	Category               string   `yaml:"category"`     // technical | policy | process | physical
	CSFFunction            string   `yaml:"csf_function"` // identify | protect | detect | respond | recover | govern
	Status                 string   `yaml:"status"`       // verified | to_do | planned | validated
	Owner                  string   `yaml:"owner"`        // platform | operator | shared
	Components             []string `yaml:"components,omitempty"`
	References             []string `yaml:"references,omitempty"`
	OperatorResponsibility string   `yaml:"operator_responsibility,omitempty"`

	// DerivedStatus is computed by the derive step — not persisted in YAML.
	// It is set when all findings for this control are resolved with evidence.
	DerivedStatus string `yaml:"-"`
}

// Group is a named collection of controls.
type Group struct {
	ID       string    `yaml:"id"`
	Title    string    `yaml:"title"`
	Controls []Control `yaml:"-"`
}

// GroupFile is the top-level structure of a catalog YAML file.
// In the real YAML, `group:` and `controls:` are siblings at the top level.
type GroupFile struct {
	Group    Group     `yaml:"group"`
	Controls []Control `yaml:"controls"`
}

// Catalog holds all loaded control groups indexed by group and control ID.
type Catalog struct {
	Groups   []Group
	Controls map[string]*Control // keyed by control ID
}

// Metadata is the top-level catalog descriptor.
type Metadata struct {
	Version string `yaml:"version"`
	Groups  []struct {
		ID   string `yaml:"id"`
		File string `yaml:"file"`
	} `yaml:"groups"`
}

// Load reads all catalog YAML files from the given directory.
func Load(catalogDir string) (*Catalog, error) {
	cat := &Catalog{
		Controls: make(map[string]*Control),
	}

	for _, subdir := range []string{"technical", "organizational"} {
		dir := filepath.Join(catalogDir, subdir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
				continue
			}

			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", entry.Name(), err)
			}

			var gf GroupFile
			if err := yaml.Unmarshal(data, &gf); err != nil {
				return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
			}

			g := gf.Group
			g.Controls = gf.Controls
			cat.Groups = append(cat.Groups, g)
			for i := range cat.Groups[len(cat.Groups)-1].Controls {
				ctrl := &cat.Groups[len(cat.Groups)-1].Controls[i]
				if _, dup := cat.Controls[ctrl.ID]; dup {
					return nil, fmt.Errorf("duplicate control ID: %s in %s", ctrl.ID, entry.Name())
				}
				cat.Controls[ctrl.ID] = ctrl
			}
		}
	}

	sort.Slice(cat.Groups, func(i, j int) bool {
		return cat.Groups[i].ID < cat.Groups[j].ID
	})

	return cat, nil
}

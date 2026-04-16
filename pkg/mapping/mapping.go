// Package mapping provides types and loading for framework-to-control mappings.
//
// Three framework mappings exist: EUDI SecReq, ISO 27001 Annex A, and GDPR.
// Each maps external requirement IDs to internal SirosID controls and tracks
// assessment results that can be derived from control and finding status.
package mapping

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// EUDIRequirement maps one EUDI SecReq requirement to controls.
type EUDIRequirement struct {
	ID          string   `yaml:"id"`
	Result      string   `yaml:"result"` // compliant | partially_compliant | non_compliant | not_applicable | not_assessed
	Status      string   `yaml:"status"` // done | in_progress | to_do
	Controls    []string `yaml:"controls"`
	Observation string   `yaml:"observation,omitempty"`
	Owner       string   `yaml:"owner"` // platform | operator | shared
}

// EUDIMapping is the top-level EUDI SecReq mapping file.
type EUDIMapping struct {
	Requirements []EUDIRequirement `yaml:"requirements"`
}

// ISOMapping entry maps one ISO 27001 Annex A control.
type ISOMapping struct {
	AnnexA   string   `yaml:"annex_a"`
	Controls []string `yaml:"controls"`
	Coverage string   `yaml:"coverage"` // full | partial | none | not_assessed
	Owner    string   `yaml:"owner"`
	Notes    string   `yaml:"notes,omitempty"`
}

// ISOFile is the top-level ISO mapping file.
type ISOFile struct {
	Mappings []ISOMapping `yaml:"mappings"`
}

// GDPRMapping entry maps one GDPR checklist item.
type GDPRMapping struct {
	MatchName string   `yaml:"match_name"`
	Controls  []string `yaml:"controls"`
	Coverage  string   `yaml:"coverage"` // full | partial | none | not_assessed
	Owner     string   `yaml:"owner"`
	Notes     string   `yaml:"notes,omitempty"`
}

// GDPRFile is the top-level GDPR mapping file.
type GDPRFile struct {
	Mappings []GDPRMapping `yaml:"mappings"`
}

// ASVSMapping entry maps one OWASP ASVS section.
type ASVSMapping struct {
	Section  string   `yaml:"section"`
	Controls []string `yaml:"controls"`
	Coverage string   `yaml:"coverage"` // full | partial | none | not_assessed
	Owner    string   `yaml:"owner"`
	Notes    string   `yaml:"notes,omitempty"`
}

// ASVSFile is the top-level OWASP ASVS mapping file.
type ASVSFile struct {
	Mappings []ASVSMapping `yaml:"mappings"`
}

// Mappings holds all loaded framework mappings.
type Mappings struct {
	EUDI *EUDIMapping
	ISO  *ISOFile
	GDPR *GDPRFile
	ASVS *ASVSFile
}

// Load reads all mapping YAML files from the given directory.
func Load(mappingsDir string) (*Mappings, error) {
	m := &Mappings{}

	// EUDI SecReq
	if data, err := readYAML(filepath.Join(mappingsDir, "eudi-secreq.yaml")); err == nil {
		var em EUDIMapping
		if err := yaml.Unmarshal(data, &em); err != nil {
			return nil, fmt.Errorf("parsing eudi-secreq.yaml: %w", err)
		}
		m.EUDI = &em
	}

	// ISO 27001 Annex A
	if data, err := readYAML(filepath.Join(mappingsDir, "iso27001-annexa.yaml")); err == nil {
		var im ISOFile
		if err := yaml.Unmarshal(data, &im); err != nil {
			return nil, fmt.Errorf("parsing iso27001-annexa.yaml: %w", err)
		}
		m.ISO = &im
	}

	// GDPR
	if data, err := readYAML(filepath.Join(mappingsDir, "gdpr.yaml")); err == nil {
		var gm GDPRFile
		if err := yaml.Unmarshal(data, &gm); err != nil {
			return nil, fmt.Errorf("parsing gdpr.yaml: %w", err)
		}
		m.GDPR = &gm
	}

	// OWASP ASVS
	if data, err := readYAML(filepath.Join(mappingsDir, "owasp-asvs.yaml")); err == nil {
		var am ASVSFile
		if err := yaml.Unmarshal(data, &am); err != nil {
			return nil, fmt.Errorf("parsing owasp-asvs.yaml: %w", err)
		}
		m.ASVS = &am
	}

	return m, nil
}

// Save writes modified mapping files back to disk.
func (m *Mappings) Save(mappingsDir string) error {
	if m.EUDI != nil {
		if err := writeYAML(filepath.Join(mappingsDir, "eudi-secreq.yaml"), m.EUDI); err != nil {
			return err
		}
	}
	if m.ISO != nil {
		if err := writeYAML(filepath.Join(mappingsDir, "iso27001-annexa.yaml"), m.ISO); err != nil {
			return err
		}
	}
	if m.GDPR != nil {
		if err := writeYAML(filepath.Join(mappingsDir, "gdpr.yaml"), m.GDPR); err != nil {
			return err
		}
	}
	if m.ASVS != nil {
		if err := writeYAML(filepath.Join(mappingsDir, "owasp-asvs.yaml"), m.ASVS); err != nil {
			return err
		}
	}
	return nil
}

func readYAML(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func writeYAML(path string, v interface{}) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", filepath.Base(path), err)
	}
	return os.WriteFile(path, data, 0644)
}

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Defaults used when no .grc.yaml exists.
var (
	DefaultRepo = "sirosfoundation/compliance"
	DefaultURL  = "https://compliance.siros.org"
	DefaultName = "Compliance Dashboard"
)

// FrameworkConfig describes one compliance framework to load and render.
type FrameworkConfig struct {
	ID              string `yaml:"id"`
	Name            string `yaml:"name"`
	CatalogFile     string `yaml:"catalog_file"`
	MappingFile     string `yaml:"mapping_file"`
	SidebarPosition int    `yaml:"sidebar_position"`

	// Mapping schema fields (generic loading/deriving).
	ListKey         string `yaml:"list_key"`          // top-level YAML key (default: "mappings")
	KeyField        string `yaml:"key_field"`         // field name for requirement ID
	StatusField     string `yaml:"status_field"`      // assessment status field (default: "coverage")
	WorkStatusField string `yaml:"work_status_field"` // optional secondary status field
	NotesField      string `yaml:"notes_field"`       // field name for notes (default: "notes")
	DeriveMode      string `yaml:"derive_mode"`       // "result" or "coverage" (default: "coverage")
	Slug            string `yaml:"slug"`              // URL slug for framework dir (default: ID)
	Source          string `yaml:"source"`            // source attribution for per-requirement pages
}

// ApplyDefaults fills in zero-value fields with sensible defaults.
func (fw *FrameworkConfig) ApplyDefaults() {
	if fw.ListKey == "" {
		fw.ListKey = "mappings"
	}
	if fw.StatusField == "" {
		fw.StatusField = "coverage"
	}
	if fw.NotesField == "" {
		fw.NotesField = "notes"
	}
	if fw.DeriveMode == "" {
		fw.DeriveMode = "coverage"
	}
	if fw.Slug == "" {
		fw.Slug = fw.ID
	}
}

// DefaultFrameworks is used when no .grc.yaml is present (backward compat).
var DefaultFrameworks = []FrameworkConfig{
	{ID: "eudi", Name: "EUDI Security Requirements", CatalogFile: "eudi-secreq.yaml", MappingFile: "eudi-secreq.yaml", SidebarPosition: 1, ListKey: "requirements", KeyField: "id", StatusField: "result", WorkStatusField: "status", NotesField: "observation", DeriveMode: "result", Slug: "eudi", Source: "ENISA – Security Requirements for European Digital Identity Wallets v0.5"},
	{ID: "iso27001", Name: "ISO 27001 Annex A", CatalogFile: "iso27001-annexa.yaml", MappingFile: "iso27001-annexa.yaml", SidebarPosition: 2, KeyField: "annex_a", Source: "ISO/IEC 27001:2022 Annex A"},
	{ID: "gdpr", Name: "GDPR Checklist", CatalogFile: "gdpr-checklist.yaml", MappingFile: "gdpr.yaml", SidebarPosition: 3, KeyField: "match_name", Source: "GDPR Checklist for Data Controllers"},
	{ID: "owasp-asvs", Name: "OWASP ASVS 4.0.3 Level 3", CatalogFile: "owasp-asvs.yaml", MappingFile: "owasp-asvs.yaml", SidebarPosition: 4, KeyField: "section", Source: "OWASP Application Security Verification Standard 4.0.3"},
}

// ProjectConfig holds project-level identity settings.
type ProjectConfig struct {
	Name string `yaml:"name"`
	Repo string `yaml:"repo"`
	URL  string `yaml:"url"`
}

// DirConfig holds directory layout settings.
type DirConfig struct {
	Dir string `yaml:"dir"`
}

// CatalogConfig holds catalog-specific settings.
type CatalogConfig struct {
	Dir           string   `yaml:"dir"`
	Subdirs       []string `yaml:"subdirs"`
	FrameworksDir string   `yaml:"frameworks_subdir"`
}

// GRCFile is the top-level .grc.yaml file structure.
type GRCFile struct {
	Project    ProjectConfig     `yaml:"project"`
	Catalog    CatalogConfig     `yaml:"catalog"`
	Mappings   DirConfig         `yaml:"mappings"`
	Audits     DirConfig         `yaml:"audits"`
	Site       DirConfig         `yaml:"site"`
	OSCAL      DirConfig         `yaml:"oscal"`
	Frameworks []FrameworkConfig `yaml:"frameworks"`
}

// Config holds the resolved runtime configuration.
type Config struct {
	Root        string
	CatalogDir  string
	MappingsDir string
	AuditsDir   string
	SiteDir     string
	OSCALDir    string

	Project          ProjectConfig
	Frameworks       []FrameworkConfig
	CatalogSubdirs   []string
	FrameworksSubdir string
}

// New loads configuration from .grc.yaml if present, falling back to defaults.
// Returns an error if .grc.yaml exists but is malformed.
func New(root string) (*Config, error) {
	cfg := &Config{
		Root:        root,
		CatalogDir:  filepath.Join(root, "catalog"),
		MappingsDir: filepath.Join(root, "mappings"),
		AuditsDir:   filepath.Join(root, "audits"),
		SiteDir:     filepath.Join(root, "site", "docs"),
		OSCALDir:    filepath.Join(root, "oscal"),
		Project: ProjectConfig{
			Name: DefaultName,
			Repo: DefaultRepo,
			URL:  DefaultURL,
		},
		CatalogSubdirs:   []string{"technical", "organizational"},
		FrameworksSubdir: "frameworks",
	}

	data, err := os.ReadFile(filepath.Join(root, ".grc.yaml"))
	if err != nil {
		// No config file — use defaults including default frameworks.
		cfg.Frameworks = make([]FrameworkConfig, len(DefaultFrameworks))
		copy(cfg.Frameworks, DefaultFrameworks)
		for i := range cfg.Frameworks {
			cfg.Frameworks[i].ApplyDefaults()
		}
		return cfg, nil
	}

	var grc GRCFile
	if err := yaml.Unmarshal(data, &grc); err != nil {
		return nil, fmt.Errorf("parsing .grc.yaml: %w", err)
	}

	// Project
	if grc.Project.Name != "" {
		cfg.Project.Name = grc.Project.Name
	}
	if grc.Project.Repo != "" {
		cfg.Project.Repo = grc.Project.Repo
	}
	if grc.Project.URL != "" {
		cfg.Project.URL = grc.Project.URL
	}

	// Directories
	if grc.Catalog.Dir != "" {
		cfg.CatalogDir = filepath.Join(root, grc.Catalog.Dir)
	}
	if len(grc.Catalog.Subdirs) > 0 {
		cfg.CatalogSubdirs = grc.Catalog.Subdirs
	}
	if grc.Catalog.FrameworksDir != "" {
		cfg.FrameworksSubdir = grc.Catalog.FrameworksDir
	}
	if grc.Mappings.Dir != "" {
		cfg.MappingsDir = filepath.Join(root, grc.Mappings.Dir)
	}
	if grc.Audits.Dir != "" {
		cfg.AuditsDir = filepath.Join(root, grc.Audits.Dir)
	}
	if grc.Site.Dir != "" {
		cfg.SiteDir = filepath.Join(root, grc.Site.Dir)
	}
	if grc.OSCAL.Dir != "" {
		cfg.OSCALDir = filepath.Join(root, grc.OSCAL.Dir)
	}

	// Frameworks
	if len(grc.Frameworks) > 0 {
		cfg.Frameworks = grc.Frameworks
	}

	// Apply defaults to each framework config.
	if len(cfg.Frameworks) == 0 {
		cfg.Frameworks = make([]FrameworkConfig, len(DefaultFrameworks))
		copy(cfg.Frameworks, DefaultFrameworks)
	}
	for i := range cfg.Frameworks {
		cfg.Frameworks[i].ApplyDefaults()
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}


// Validate checks the config for common mistakes.
func (c *Config) Validate() error {
	seen := make(map[string]bool)
	for _, fw := range c.Frameworks {
		if fw.ID == "" {
			return fmt.Errorf("framework missing required field: id")
		}
		if seen[fw.ID] {
			return fmt.Errorf("duplicate framework id: %s", fw.ID)
		}
		seen[fw.ID] = true
		if fw.KeyField == "" {
			return fmt.Errorf("framework %s: missing required field: key_field", fw.ID)
		}
	}
	return nil
}

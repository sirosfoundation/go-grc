package config

import (
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
func New(root string) *Config {
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
		return cfg // no config file — use defaults
	}

	var grc GRCFile
	if err := yaml.Unmarshal(data, &grc); err != nil {
		return cfg // malformed — use defaults
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

	return cfg
}

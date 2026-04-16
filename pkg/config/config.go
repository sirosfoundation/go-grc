package config

import "path/filepath"

const DefaultRepo = "sirosfoundation/compliance"
const ComplianceURL = "https://compliance.siros.org"

type Config struct {
	Root        string
	CatalogDir  string
	MappingsDir string
	AuditsDir   string
	SiteDir     string
	OSCALDir    string
}

func New(root string) *Config {
	return &Config{
		Root:        root,
		CatalogDir:  filepath.Join(root, "catalog"),
		MappingsDir: filepath.Join(root, "mappings"),
		AuditsDir:   filepath.Join(root, "audits"),
		SiteDir:     filepath.Join(root, "site", "docs"),
		OSCALDir:    filepath.Join(root, "oscal"),
	}
}

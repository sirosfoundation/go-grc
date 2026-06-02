package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew_NoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := New(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project.Name != DefaultName {
		t.Errorf("expected default name %q, got %q", DefaultName, cfg.Project.Name)
	}
	if len(cfg.Frameworks) != len(DefaultFrameworks) {
		t.Errorf("expected %d frameworks, got %d", len(DefaultFrameworks), len(cfg.Frameworks))
	}
}

func TestNew_ValidFile(t *testing.T) {
	dir := t.TempDir()
	yaml := `project:
  name: "Test"
  repo: "org/repo"
frameworks:
  - id: test
    name: "Test Framework"
    key_field: id
    mapping_file: test.yaml
    catalog_file: test.yaml
`
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project.Name != "Test" {
		t.Errorf("expected project name 'Test', got %q", cfg.Project.Name)
	}
	if len(cfg.Frameworks) != 1 || cfg.Frameworks[0].ID != "test" {
		t.Errorf("expected 1 framework 'test', got %v", cfg.Frameworks)
	}
}

func TestNew_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := New(dir)
	if err == nil {
		t.Fatal("expected error for malformed YAML, got nil")
	}
}

func TestNew_DuplicateFrameworkID(t *testing.T) {
	dir := t.TempDir()
	yaml := `frameworks:
  - id: dupe
    name: "First"
    key_field: id
    mapping_file: a.yaml
    catalog_file: a.yaml
  - id: dupe
    name: "Second"
    key_field: id
    mapping_file: b.yaml
    catalog_file: b.yaml
`
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := New(dir)
	if err == nil {
		t.Fatal("expected error for duplicate framework ID, got nil")
	}
}

func TestNew_MissingKeyField(t *testing.T) {
	dir := t.TempDir()
	yaml := `frameworks:
  - id: test
    name: "Test"
    mapping_file: test.yaml
    catalog_file: test.yaml
`
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := New(dir)
	if err == nil {
		t.Fatal("expected error for missing key_field, got nil")
	}
}

func TestApplyDefaults(t *testing.T) {
	fw := FrameworkConfig{ID: "test", KeyField: "id"}
	fw.ApplyDefaults()
	if fw.ListKey != "mappings" {
		t.Errorf("expected default list_key 'mappings', got %q", fw.ListKey)
	}
	if fw.Slug != "test" {
		t.Errorf("expected slug 'test', got %q", fw.Slug)
	}
	if fw.DeriveMode != "coverage" {
		t.Errorf("expected default derive_mode 'coverage', got %q", fw.DeriveMode)
	}
}

func TestNew_AllDirectoryOverrides(t *testing.T) {
	dir := t.TempDir()
	yaml := `project:
  name: "Override Test"
  repo: "org/repo"
  url: "https://example.com"
catalog:
  dir: "my-catalog"
  subdirs: ["a", "b"]
  frameworks_subdir: "my-frameworks"
mappings:
  dir: "my-mappings"
audits:
  dir: "my-audits"
site:
  dir: "my-site"
oscal:
  dir: "my-oscal"
frameworks:
  - id: test
    name: "Test"
    key_field: id
    mapping_file: test.yaml
    catalog_file: test.yaml
risk_register:
  dir: "my-risks"
  files: ["r.yaml"]
`
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Project.Name != "Override Test" {
		t.Errorf("name: got %q", cfg.Project.Name)
	}
	if cfg.Project.Repo != "org/repo" {
		t.Errorf("repo: got %q", cfg.Project.Repo)
	}
	if cfg.Project.URL != "https://example.com" {
		t.Errorf("url: got %q", cfg.Project.URL)
	}
	if cfg.CatalogDir != filepath.Join(dir, "my-catalog") {
		t.Errorf("catalog dir: got %q", cfg.CatalogDir)
	}
	if len(cfg.CatalogSubdirs) != 2 {
		t.Errorf("catalog subdirs: got %v", cfg.CatalogSubdirs)
	}
	if cfg.FrameworksSubdir != "my-frameworks" {
		t.Errorf("frameworks subdir: got %q", cfg.FrameworksSubdir)
	}
	if cfg.MappingsDir != filepath.Join(dir, "my-mappings") {
		t.Errorf("mappings dir: got %q", cfg.MappingsDir)
	}
	if cfg.AuditsDir != filepath.Join(dir, "my-audits") {
		t.Errorf("audits dir: got %q", cfg.AuditsDir)
	}
	if cfg.SiteDir != filepath.Join(dir, "my-site") {
		t.Errorf("site dir: got %q", cfg.SiteDir)
	}
	if cfg.OSCALDir != filepath.Join(dir, "my-oscal") {
		t.Errorf("oscal dir: got %q", cfg.OSCALDir)
	}
	if cfg.RiskDir != filepath.Join(dir, "my-risks") {
		t.Errorf("risk dir: got %q", cfg.RiskDir)
	}
}

func TestNew_Profiles(t *testing.T) {
	dir := t.TempDir()
	yaml := `profiles:
  - id: native_only
    label: "Native Only"
    default: true
  - id: web
    label: "Web"
    inherits: native_only
frameworks:
  - id: test
    name: "Test"
    key_field: id
    mapping_file: test.yaml
    catalog_file: test.yaml
`
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := New(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DefaultProfile() != "native_only" {
		t.Errorf("default profile: got %q", cfg.DefaultProfile())
	}
	ids := cfg.ProfileIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(ids))
	}
	if !cfg.HasProfile("native_only") {
		t.Error("HasProfile(native_only) should be true")
	}
	if cfg.HasProfile("nonexistent") {
		t.Error("HasProfile(nonexistent) should be false")
	}
}

func TestNew_DuplicateProfileID(t *testing.T) {
	dir := t.TempDir()
	yaml := `profiles:
  - id: dupe
    label: "A"
  - id: dupe
    label: "B"
`
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := New(dir)
	if err == nil {
		t.Fatal("expected error for duplicate profile ID")
	}
}

func TestNew_ProfileInheritsUnknown(t *testing.T) {
	dir := t.TempDir()
	yaml := `profiles:
  - id: child
    label: "Child"
    inherits: nonexistent
`
	if err := os.WriteFile(filepath.Join(dir, ".grc.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := New(dir)
	if err == nil {
		t.Fatal("expected error for inherits unknown profile")
	}
}

func TestDefaultProfile_NoProfiles(t *testing.T) {
	cfg := &Config{}
	if cfg.DefaultProfile() != "" {
		t.Error("expected empty default profile when no profiles defined")
	}
}

func TestDefaultProfile_NoDefault(t *testing.T) {
	cfg := &Config{Profiles: []ProfileConfig{{ID: "a"}, {ID: "b"}}}
	if cfg.DefaultProfile() != "a" {
		t.Errorf("expected first profile 'a' as default, got %q", cfg.DefaultProfile())
	}
}

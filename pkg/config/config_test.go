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

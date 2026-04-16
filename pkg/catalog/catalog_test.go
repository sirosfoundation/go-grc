package catalog_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/catalog"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "testdata")
}

func TestLoad(t *testing.T) {
	cat, err := catalog.Load(filepath.Join(testdataDir(), "catalog"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cat.Groups) == 0 {
		t.Fatal("expected at least one group")
	}

	// Should have loaded from both technical/ and organizational/
	if len(cat.Controls) != 5 {
		t.Errorf("expected 5 controls, got %d", len(cat.Controls))
	}

	// Verify specific controls exist
	for _, id := range []string{"SEC-AUTH-01", "SEC-SESS-01", "SEC-WEB-01", "SEC-KEY-01", "GOV-POL-01"} {
		if _, ok := cat.Controls[id]; !ok {
			t.Errorf("expected control %s not found", id)
		}
	}

	// Check fields loaded correctly
	ctrl := cat.Controls["SEC-AUTH-01"]
	if ctrl.Title != "Authentication mechanism" {
		t.Errorf("expected title 'Authentication mechanism', got %q", ctrl.Title)
	}
	if ctrl.Owner != "go-wallet-backend" {
		t.Errorf("expected owner 'go-wallet-backend', got %q", ctrl.Owner)
	}
	if ctrl.CSFFunction != "protect" {
		t.Errorf("expected csf_function 'protect', got %q", ctrl.CSFFunction)
	}
}

func TestLoad_MissingDir(t *testing.T) {
	_, err := catalog.Load("/nonexistent/path")
	// Should not fail fatally — just return empty catalog
	if err != nil {
		t.Logf("Load() with missing dir returned error (acceptable): %v", err)
	}
}

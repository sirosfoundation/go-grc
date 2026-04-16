package mapping_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func testdataDir() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "testdata")
}

func TestLoad(t *testing.T) {
	m, err := mapping.Load(filepath.Join(testdataDir(), "mappings"))
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if m.EUDI == nil {
		t.Fatal("EUDI mapping should be loaded")
	}
	if len(m.EUDI.Requirements) != 2 {
		t.Errorf("expected 2 EUDI requirements, got %d", len(m.EUDI.Requirements))
	}
	if m.EUDI.Requirements[0].ID != "WTE_07" {
		t.Errorf("first EUDI req: got %q, want WTE_07", m.EUDI.Requirements[0].ID)
	}

	if m.ISO == nil {
		t.Fatal("ISO mapping should be loaded")
	}
	if len(m.ISO.Mappings) != 2 {
		t.Errorf("expected 2 ISO mappings, got %d", len(m.ISO.Mappings))
	}

	if m.GDPR == nil {
		t.Fatal("GDPR mapping should be loaded")
	}
	if len(m.GDPR.Mappings) != 1 {
		t.Errorf("expected 1 GDPR mapping, got %d", len(m.GDPR.Mappings))
	}
}

func TestSave(t *testing.T) {
	tmp := t.TempDir()

	// Copy fixtures
	for _, name := range []string{"eudi-secreq.yaml", "iso27001-annexa.yaml", "gdpr.yaml"} {
		data, err := os.ReadFile(filepath.Join(testdataDir(), "mappings", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(tmp, name), data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	m, err := mapping.Load(tmp)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Modify and save
	m.EUDI.Requirements[0].Result = "compliant"
	if err := m.Save(tmp); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Re-load and verify
	m2, err := mapping.Load(tmp)
	if err != nil {
		t.Fatalf("Re-load error: %v", err)
	}
	if m2.EUDI.Requirements[0].Result != "compliant" {
		t.Errorf("expected compliant after save, got %q", m2.EUDI.Requirements[0].Result)
	}
}

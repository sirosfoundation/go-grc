package oscal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/config"
)

func TestOSCALCommand(t *testing.T) {
	// Use the testdata directory
	root := filepath.Join("..", "..", "..", "testdata")
	cfg, err := config.New(root)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	// Ensure oscal dir exists
	os.MkdirAll(cfg.OSCALDir, 0o755)
	defer os.RemoveAll(cfg.OSCALDir)

	err = run(root)
	if err != nil {
		t.Fatalf("oscal export failed: %v", err)
	}

	outPath := filepath.Join(cfg.OSCALDir, "component-definition.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	var doc ComponentDefinitionDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing output JSON: %v", err)
	}

	cd := doc.ComponentDefinition
	if cd.Metadata.OSCALVersion != "1.1.2" {
		t.Errorf("oscal-version = %q, want 1.1.2", cd.Metadata.OSCALVersion)
	}
	if len(cd.Components) != 1 {
		t.Fatalf("components = %d, want 1", len(cd.Components))
	}
	ci := cd.Components[0].ControlImplementations
	if len(ci) != 1 {
		t.Fatalf("control-implementations = %d, want 1", len(ci))
	}
	reqs := ci[0].ImplementedRequirements
	if len(reqs) == 0 {
		t.Fatal("no implemented-requirements generated")
	}

	// Verify each requirement has required props
	for _, r := range reqs {
		if r.UUID == "" {
			t.Errorf("requirement %s has empty UUID", r.ControlID)
		}
		if r.ControlID == "" {
			t.Error("requirement has empty control-id")
		}
		propNames := make(map[string]bool)
		for _, p := range r.Props {
			propNames[p.Name] = true
		}
		for _, required := range []string{"status", "category", "csf_function", "owner", "group"} {
			if !propNames[required] {
				t.Errorf("requirement %s missing prop %q", r.ControlID, required)
			}
		}
	}

	// Verify UUIDs are deterministic (run again, same UUIDs)
	err = run(root)
	if err != nil {
		t.Fatalf("second oscal export failed: %v", err)
	}
	data2, _ := os.ReadFile(outPath)
	var doc2 ComponentDefinitionDoc
	json.Unmarshal(data2, &doc2)
	if doc2.ComponentDefinition.UUID != cd.UUID {
		t.Error("top-level UUID changed between runs (not deterministic)")
	}
}

func TestUUID5Deterministic(t *testing.T) {
	a := uuid5(nsURL, "https://example.com/test")
	b := uuid5(nsURL, "https://example.com/test")
	if a != b {
		t.Errorf("uuid5 not deterministic: %s != %s", a, b)
	}
	c := uuid5(nsURL, "https://example.com/other")
	if a == c {
		t.Error("uuid5 produced same UUID for different inputs")
	}
}

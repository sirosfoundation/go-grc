package catalog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/catalog"
)

func writeFramework(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "frameworks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frameworks", name+".yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadFrameworkCatalog(t *testing.T) {
	fc, err := catalog.LoadFrameworkCatalog(filepath.Join(testdataDir(), "catalog"), "eudi-secreq", nil)
	if err != nil {
		t.Fatal(err)
	}
	if fc.Framework.ID != "eudi" || fc.Framework.Version != "0.5" || fc.Framework.Source != "ENISA" {
		t.Errorf("framework metadata = %+v", fc.Framework)
	}
	if len(fc.Requirements) != 1 {
		t.Fatalf("requirements = %d", len(fc.Requirements))
	}
	r := fc.ByID["WTE_07"]
	if r == nil || r.Title != "Session management" || r.Section != "5.1" || r.Description != "The wallet shall implement session timeout." {
		t.Errorf("WTE_07 = %+v", r)
	}
	if r != &fc.Requirements[0] {
		t.Error("ByID should point into Requirements")
	}
}

func TestLoadFrameworkCatalogMissingFileIsNil(t *testing.T) {
	fc, err := catalog.LoadFrameworkCatalog(t.TempDir(), "nope", nil)
	if fc != nil || err != nil {
		t.Errorf("missing file: %v %v", fc, err)
	}
	fc, err = catalog.LoadFrameworkCatalog(t.TempDir(), "nope", []string{"a"})
	if fc != nil || err != nil {
		t.Errorf("missing file with sections: %v %v", fc, err)
	}
}

func TestLoadFrameworkCatalogErrors(t *testing.T) {
	dir := writeFramework(t, "bad", "framework: [unterminated")
	if _, err := catalog.LoadFrameworkCatalog(dir, "bad", nil); err == nil || !strings.Contains(err.Error(), "parsing bad") {
		t.Errorf("plain parse err = %v", err)
	}
	if _, err := catalog.LoadFrameworkCatalog(dir, "bad", []string{"requirements"}); err == nil || !strings.Contains(err.Error(), "parsing bad") {
		t.Errorf("multi parse err = %v", err)
	}

	// A path that exists but cannot be read as a file (a directory).
	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "frameworks", "isdir.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.LoadFrameworkCatalog(dir, "isdir", nil); err == nil || !strings.Contains(err.Error(), "reading isdir") {
		t.Errorf("read err = %v", err)
	}

	// A section that is not a list of requirements.
	dir = writeFramework(t, "wrong", "framework:\n  id: x\nsection_a: not-a-list\n")
	if _, err := catalog.LoadFrameworkCatalog(dir, "wrong", []string{"section_a"}); err == nil || !strings.Contains(err.Error(), `parsing section "section_a" in wrong`) {
		t.Errorf("section err = %v", err)
	}
}

func TestLoadFrameworkCatalogMultiSection(t *testing.T) {
	dir := writeFramework(t, "multi", `framework:
  id: owasp
  title: OWASP ASVS
  version: "4.0"
  source: owasp.org
v1_architecture:
  - id: V1.1.1
    title: SDLC
    section: "1.1"
    description: Secure SDLC is used.
v2_authentication:
  - id: V2.1.1
    title: Password length
    section: "2.1"
    description: Passwords are at least 12 characters.
  - id: V2.1.2
    title: Password max
ignored_section:
  - id: NOPE
    title: not requested
`)
	fc, err := catalog.LoadFrameworkCatalog(dir, "multi", []string{"v1_architecture", "missing_section", "v2_authentication"})
	if err != nil {
		t.Fatal(err)
	}
	if fc.Framework.ID != "owasp" || fc.Framework.Title != "OWASP ASVS" || fc.Framework.Version != "4.0" {
		t.Errorf("metadata = %+v", fc.Framework)
	}
	var got []string
	for _, r := range fc.Requirements {
		got = append(got, r.ID)
	}
	if strings.Join(got, ",") != "V1.1.1,V2.1.1,V2.1.2" {
		t.Errorf("requirements in section order = %v", got)
	}
	if fc.ByID["NOPE"] != nil {
		t.Error("unrequested section must not be loaded")
	}
	if r := fc.ByID["V2.1.1"]; r == nil || r.Description != "Passwords are at least 12 characters." || r.Section != "2.1" {
		t.Errorf("V2.1.1 = %+v", r)
	}

	// Without a framework block the metadata is simply empty.
	dir = writeFramework(t, "nometa", "sec:\n  - id: A\n    title: T\n")
	fc, err = catalog.LoadFrameworkCatalog(dir, "nometa", []string{"sec"})
	if err != nil || fc.Framework.ID != "" || fc.ByID["A"] == nil {
		t.Errorf("nometa: %+v %v", fc, err)
	}
}

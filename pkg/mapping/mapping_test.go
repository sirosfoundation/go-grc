package mapping_test

import (
"os"
"path/filepath"
"runtime"
"testing"

"github.com/sirosfoundation/go-grc/pkg/config"
"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func testdataDir() string {
_, f, _, _ := runtime.Caller(0)
return filepath.Join(filepath.Dir(f), "..", "..", "testdata")
}

func testFrameworks() []config.FrameworkConfig {
cfg, _ := config.New(testdataDir())
return cfg.Frameworks
}

func TestLoad(t *testing.T) {
fws := testFrameworks()
m, err := mapping.Load(filepath.Join(testdataDir(), "mappings"), fws)
if err != nil {
t.Fatalf("Load() error: %v", err)
}

// EUDI
eudi := m["eudi"]
if eudi == nil {
t.Fatal("EUDI mapping should be loaded")
}
if len(eudi.Entries) != 2 {
t.Errorf("expected 2 EUDI entries, got %d", len(eudi.Entries))
}
if eudi.Entries[0].Key != "WTE_07" {
t.Errorf("first EUDI entry key: got %q, want WTE_07", eudi.Entries[0].Key)
}
if eudi.Entries[0].Status != "not_assessed" {
t.Errorf("first EUDI entry status: got %q, want not_assessed", eudi.Entries[0].Status)
}
if eudi.Entries[0].WorkStatus != "to_do" {
t.Errorf("first EUDI entry work_status: got %q, want to_do", eudi.Entries[0].WorkStatus)
}

// ISO
iso := m["iso27001"]
if iso == nil {
t.Fatal("ISO mapping should be loaded")
}
if len(iso.Entries) != 2 {
t.Errorf("expected 2 ISO entries, got %d", len(iso.Entries))
}

// GDPR
gdpr := m["gdpr"]
if gdpr == nil {
t.Fatal("GDPR mapping should be loaded")
}
if len(gdpr.Entries) != 1 {
t.Errorf("expected 1 GDPR entry, got %d", len(gdpr.Entries))
}
}

func TestSave(t *testing.T) {
tmp := t.TempDir()
fws := testFrameworks()

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

m, err := mapping.Load(tmp, fws)
if err != nil {
t.Fatalf("Load() error: %v", err)
}

// Modify and save
m["eudi"].Entries[0].Status = "compliant"
if err := m.Save(tmp, fws); err != nil {
t.Fatalf("Save() error: %v", err)
}

// Re-load and verify
m2, err := mapping.Load(tmp, fws)
if err != nil {
t.Fatalf("Re-load error: %v", err)
}
if m2["eudi"].Entries[0].Status != "compliant" {
t.Errorf("expected compliant after save, got %q", m2["eudi"].Entries[0].Status)
}
}

func TestSave_PreservesExtraFields(t *testing.T) {
tmp := t.TempDir()
fws := testFrameworks()

// Copy EUDI fixture (has framework+version top-level fields)
data, err := os.ReadFile(filepath.Join(testdataDir(), "mappings", "eudi-secreq.yaml"))
if err != nil {
t.Fatal(err)
}
if err := os.WriteFile(filepath.Join(tmp, "eudi-secreq.yaml"), data, 0644); err != nil {
t.Fatal(err)
}
// Copy other fixtures needed for Load
for _, name := range []string{"iso27001-annexa.yaml", "gdpr.yaml"} {
d, err := os.ReadFile(filepath.Join(testdataDir(), "mappings", name))
if err != nil {
t.Fatal(err)
}
if err := os.WriteFile(filepath.Join(tmp, name), d, 0644); err != nil {
t.Fatal(err)
}
}

m, err := mapping.Load(tmp, fws)
if err != nil {
t.Fatalf("Load: %v", err)
}

eudi := m["eudi"]
// Verify Extra captured the framework/version keys
if eudi.Extra["framework"] != "EUDI ARF" {
t.Errorf("Extra[framework] = %v, want 'EUDI ARF'", eudi.Extra["framework"])
}
if eudi.Extra["version"] != "1.4.1" {
t.Errorf("Extra[version] = %v, want '1.4.1'", eudi.Extra["version"])
}

// Save and re-read raw YAML to confirm fields are preserved
if err := m.Save(tmp, fws); err != nil {
t.Fatalf("Save: %v", err)
}

raw, err := os.ReadFile(filepath.Join(tmp, "eudi-secreq.yaml"))
if err != nil {
t.Fatal(err)
}
content := string(raw)
if !contains(content, "framework:") {
t.Error("saved file missing 'framework:' key")
}
if !contains(content, "version:") {
t.Error("saved file missing 'version:' key")
}
}

func contains(s, substr string) bool {
return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstr(s, substr))
}

func findSubstr(s, sub string) bool {
for i := 0; i+len(sub) <= len(s); i++ {
if s[i:i+len(sub)] == sub {
return true
}
}
return false
}

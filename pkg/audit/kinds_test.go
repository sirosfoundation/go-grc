package audit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/audit"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func finding(id string) string {
	return "audit:\n  id: X\nfindings:\n  - id: " + id + "\n    title: t\n    severity: low\n    status: open\n    owner: o\n    controls: [SID-1]\n"
}

func TestLoadDirsKindsAndCycleLayout(t *testing.T) {
	root := t.TempDir()
	// Real audit cycle: findings in audit.yaml; soa.yaml and references/ are not findings files.
	writeFile(t, filepath.Join(root, "audits", "iso-2026", "audit.yaml"), finding("AUD-1"))
	writeFile(t, filepath.Join(root, "audits", "iso-2026", "soa.yaml"), "soa:\n  id: S\nrequirements: []\n")
	writeFile(t, filepath.Join(root, "audits", "iso-2026", "references", "notes.yaml"), "not: findings\n")
	// Legacy flat file in audits/ is still a real audit.
	writeFile(t, filepath.Join(root, "audits", "flat.yaml"), finding("AUD-2"))
	writeFile(t, filepath.Join(root, "assessments", "self.yaml"), finding("ASM-1"))

	set, err := audit.LoadDirs(filepath.Join(root, "audits"), filepath.Join(root, "assessments"))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]audit.Kind{}
	for id, ref := range set.FindingsByID {
		kinds[id] = ref.File.Kind
	}
	want := map[string]audit.Kind{"AUD-1": audit.KindAudit, "AUD-2": audit.KindAudit, "ASM-1": audit.KindAssessment}
	if len(kinds) != len(want) {
		t.Fatalf("findings = %v, want %v", kinds, want)
	}
	for id, k := range want {
		if kinds[id] != k {
			t.Errorf("%s kind = %q, want %q", id, kinds[id], k)
		}
	}
	if len(set.FindingsByControl["SID-1"]) != 3 {
		t.Errorf("control index covers %d findings, want all 3", len(set.FindingsByControl["SID-1"]))
	}
}

func TestAuditsOnlyDropsAssessments(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "audits", "c", "audit.yaml"), finding("AUD-1"))
	writeFile(t, filepath.Join(root, "assessments", "self.yaml"), finding("ASM-1"))
	set, err := audit.LoadDirs(filepath.Join(root, "audits"), filepath.Join(root, "assessments"))
	if err != nil {
		t.Fatal(err)
	}
	pub := set.AuditsOnly()
	if _, leaked := pub.FindingsByID["ASM-1"]; leaked {
		t.Error("assessment finding survived AuditsOnly in FindingsByID")
	}
	if _, ok := pub.FindingsByID["AUD-1"]; !ok {
		t.Error("audit finding lost by AuditsOnly")
	}
	for _, f := range pub.Files {
		if f.Kind != audit.KindAudit {
			t.Errorf("assessment file %s survived AuditsOnly", f.Path)
		}
	}
	for _, refs := range pub.FindingsByControl {
		for _, r := range refs {
			if r.Finding.ID == "ASM-1" {
				t.Error("assessment finding survived AuditsOnly in FindingsByControl")
			}
		}
	}
}

func TestLoadDirsMissingAssessmentsDirIsFine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "audits", "a.yaml"), finding("AUD-1"))
	if _, err := audit.LoadDirs(filepath.Join(root, "audits"), filepath.Join(root, "nope")); err != nil {
		t.Errorf("missing assessments dir: %v", err)
	}
	if _, err := audit.LoadDirs(filepath.Join(root, "missing"), ""); err == nil {
		t.Error("missing audits dir should still be an error")
	}
}

func TestLoadDirsDuplicateIDAcrossKinds(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "audits", "a.yaml"), finding("DUP-1"))
	writeFile(t, filepath.Join(root, "assessments", "b.yaml"), finding("DUP-1"))
	_, err := audit.LoadDirs(filepath.Join(root, "audits"), filepath.Join(root, "assessments"))
	if err == nil || !strings.Contains(err.Error(), "duplicate finding ID") {
		t.Errorf("err = %v, want duplicate finding ID", err)
	}
}

func TestAssessmentDirectoryLayoutAndHeaders(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "audits", "a1", "audit.yaml"), finding("AUD-1"))
	writeFile(t, filepath.Join(root, "assessments", "gdpr", "assessment.yaml"),
		"assessment:\n  id: G\n  catalogs: [gdpr]\nfindings:\n  - id: ASM-1\n    title: t\n    severity: low\n    status: open\n    owner: o\n    controls: []\n")
	// Older per-assessment layout (audit.yaml inside assessments/) still loads.
	writeFile(t, filepath.Join(root, "assessments", "old", "audit.yaml"), finding("ASM-2"))

	set, err := audit.LoadDirs(filepath.Join(root, "audits"), filepath.Join(root, "assessments"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]audit.Kind{}
	for _, f := range set.Files {
		names[f.Name] = f.Kind
	}
	if names["a1"] != audit.KindAudit || names["gdpr"] != audit.KindAssessment || names["old"] != audit.KindAssessment {
		t.Errorf("names/kinds wrong: %v", names)
	}
	for _, f := range set.Files {
		if f.Name == "gdpr" {
			if m := f.Data.Meta(); m.ID != "G" || len(m.Catalogs) != 1 {
				t.Errorf("assessment header not read: %+v", m)
			}
		}
	}
	// Saving keeps the header under the key it was written with.
	for i := range set.Files {
		if set.Files[i].Name == "gdpr" {
			if err := set.Files[i].Save(); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(set.Files[i].Path)
			if !strings.Contains(string(b), "assessment:") || strings.Contains(string(b), "\naudit:") {
				t.Errorf("Save changed the header key:\n%s", b)
			}
		}
	}
}

func TestAssessmentHeaderInAuditsDirIsRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "audits", "x", "audit.yaml"), "assessment:\n  id: X\nfindings: []\n")
	if _, err := audit.LoadDirs(filepath.Join(root, "audits"), ""); err == nil || !strings.Contains(err.Error(), "assessments directory") {
		t.Errorf("want a clear error for an assessment in the audits dir, got %v", err)
	}
}

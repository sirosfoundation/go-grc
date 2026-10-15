package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/policy"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVisibilityIsIndependentOfStatus(t *testing.T) {
	cases := map[string]bool{
		"visibility: public\nstatus: draft":      true,
		"visibility: public\nstatus: approved":   true,
		"visibility: private\nstatus: approved":  false,
		"visibility: private\nstatus: draft":     false,
		"status: approved":                       false, // missing visibility fails closed
		"visibility: Public\nstatus: approved":   false, // not exact
		"visibility: internal\nstatus: approved": false,
	}
	for fm, want := range cases {
		dir := t.TempDir()
		write(t, dir, "p.md", "---\n"+fm+"\n---\n# P\n")
		ps, err := policy.Load(dir)
		if err != nil || len(ps) != 1 {
			t.Fatalf("%q: %v %v", fm, ps, err)
		}
		if ps[0].Public() != want {
			t.Errorf("%q: Public() = %v, want %v", fm, ps[0].Public(), want)
		}
	}
}

func TestLoadParsesMetaAndTitle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.md", "---\nvisibility: public\nstatus: approved\napproved_date: 2026-09-10\nsidebar_position: 3\n---\n# Heading Title\nbody\n")
	write(t, dir, "b.md", "# No front matter\n")
	write(t, dir, "README.md", "ignored")
	ps, err := policy.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("want 2 policies, got %d", len(ps))
	}
	a := ps[0]
	if a.Title != "Heading Title" || a.ApprovedDate != "2026-09-10" || a.State() != "approved" {
		t.Errorf("unexpected parse: %+v", a)
	}
	if _, ok := a.Meta["sidebar_position"]; !ok {
		t.Error("docusaurus front matter was dropped")
	}
	if _, ok := a.Meta["visibility"]; ok {
		t.Error("visibility must not be passed through to the site")
	}
	if ps[1].State() != "draft" || ps[1].Public() {
		t.Error("document without front matter must be a private draft")
	}
}

func TestValidate(t *testing.T) {
	for fm, want := range map[string]string{
		"status: draft":                        "visibility is not set",
		"visibility: public":                   "status is not set",
		"visibility: nope\nstatus: draft":      `visibility is "nope"`,
		"visibility: public\nstatus: nope":     `status is "nope"`,
		"visibility: public\nstatus: approved": "approved_date is empty",
	} {
		dir := t.TempDir()
		write(t, dir, "p.md", "---\n"+fm+"\n---\n# P\n")
		ps, _ := policy.Load(dir)
		if got := strings.Join(ps[0].Validate(), "\n"); !strings.Contains(got, want) {
			t.Errorf("%q: want %q in %q", fm, want, got)
		}
	}
	dir := t.TempDir()
	write(t, dir, "p.md", "---\nvisibility: private\nstatus: approved\napproved_date: 2026-01-02\n---\n# P\n")
	ps, _ := policy.Load(dir)
	if p := ps[0].Validate(); len(p) != 0 {
		t.Errorf("valid policy reported %v", p)
	}
}

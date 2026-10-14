package result_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/result"
)

func TestPublishedOnlyWhenIssued(t *testing.T) {
	for st, want := range map[string]bool{"issued": true, "in_audit": false, "planned": false, "withdrawn": false, "Issued": false, "": false} {
		r := result.Result{Header: result.Header{Status: st}}
		if r.Published() != want {
			t.Errorf("status %q: Published() = %v, want %v", st, r.Published(), want)
		}
	}
}

func TestSafeFile(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside.pdf")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.pdf")); err != nil {
		t.Fatal(err)
	}
	// a directory symlink pointing out of the audit directory
	if err := os.Symlink(filepath.Dir(dir), filepath.Join(dir, "up")); err != nil {
		t.Fatal(err)
	}
	if _, err := result.SafeFile(dir, "up/outside.pdf"); err == nil {
		t.Error("file reached through an escaping directory symlink was accepted")
	}
	if _, err := result.SafeFile(dir, "ok.pdf"); err != nil {
		t.Errorf("valid file rejected: %v", err)
	}
	for _, bad := range []string{"", "/etc/passwd", "../outside.pdf", "a/../../outside.pdf", "run.sh", "link.pdf", "missing.pdf"} {
		if _, err := result.SafeFile(dir, bad); err == nil {
			t.Errorf("SafeFile(%q) accepted", bad)
		}
	}
}

func TestLoadAndValidate(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "iso-2026")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "result.yaml"), []byte("result:\n  status: issued\n  scheme: S\n  body: B\n  issued: 2026-11-01\n  valid_until: not-a-date\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := result.Load(root)
	if err != nil || len(rs) != 1 || rs[0].Cycle != "iso-2026" {
		t.Fatalf("load: %v %v", rs, err)
	}
	if got := strings.Join(rs[0].Validate(), "\n"); !strings.Contains(got, `valid_until "not-a-date"`) || strings.Contains(got, "issued date") {
		t.Errorf("validate: %s", got)
	}
	if rs, err := result.Load(filepath.Join(root, "nope")); err != nil || rs != nil {
		t.Errorf("missing dir: %v %v", rs, err)
	}
}

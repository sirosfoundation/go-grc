// Package testutil holds helpers shared by the command tests.
package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CopyFixtureWithoutTreatment copies the fixture tree at src into a temporary
// root, with the treatment_action block removed from the risk register, and
// returns the new root.
func CopyFixtureWithoutTreatment(t *testing.T, src string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "risk-register", "platform.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	i := strings.Index(s, "    treatment_action:")
	j := strings.Index(s, "    tracking:")
	if i < 0 || j < i {
		t.Fatal("fixture has no treatment_action to remove")
	}
	if err := os.WriteFile(path, []byte(s[:i]+s[j:]), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

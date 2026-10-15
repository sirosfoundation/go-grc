package policy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.org", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE=2026-10-15T14:03:00+02:00", "GIT_AUTHOR_DATE=2026-10-15T14:03:00+02:00")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLastChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	a, b := filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")
	_ = os.WriteFile(a, []byte("a1"), 0o644)
	_ = os.WriteFile(b, []byte("b1"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "both")
	_ = os.WriteFile(b, []byte("b2"), 0o644)
	gitRun(t, dir, "commit", "-q", "-am", "only b")

	ca, cb := LastChange(context.Background(), a), LastChange(context.Background(), b)
	if ca == nil || cb == nil {
		t.Fatalf("want changes for tracked files, got %v %v", ca, cb)
	}
	if ca.SHA == cb.SHA {
		t.Error("a.md was not touched by the second commit and must keep the first commit's SHA")
	}
	if got := ca.Time.Format("2006-01-02 15:04"); got != "2026-10-15 12:03" {
		t.Errorf("time = %s, want UTC 2026-10-15 12:03", got)
	}

	untracked := filepath.Join(dir, "c.md")
	_ = os.WriteFile(untracked, []byte("c"), 0o644)
	if LastChange(context.Background(), untracked) != nil {
		t.Error("an untracked file has no last change")
	}

	// A shallow clone would report the newest commit for every file; refuse.
	shallow := filepath.Join(t.TempDir(), "s")
	if out, err := exec.Command("git", "clone", "-q", "--depth", "1", "file://"+dir, shallow).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if LastChange(context.Background(), filepath.Join(shallow, "a.md")) != nil {
		t.Error("shallow clone must yield no change rather than a wrong one")
	}
}

func TestLastChangeOutsideGit(t *testing.T) {
	f := filepath.Join(t.TempDir(), "x.md")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	if LastChange(context.Background(), f) != nil {
		t.Error("a file outside a git repository has no last change")
	}
}

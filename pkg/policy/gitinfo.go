package policy

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Change is the last commit that touched a policy file.
type Change struct {
	SHA  string
	Time time.Time
}

// LastChange returns the last commit that changed the file at path, or nil
// when that cannot be told reliably: git is missing, the file is not
// tracked, or the clone is shallow (a shallow history would name the newest
// commit for every file, which is wrong rather than merely absent).
func LastChange(ctx context.Context, path string) *Change {
	dir := filepath.Dir(path)
	git := func(args ...string) (string, bool) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err == nil
	}
	if shallow, ok := git("rev-parse", "--is-shallow-repository"); !ok || shallow != "false" {
		return nil
	}
	out, ok := git("log", "-1", "--format=%H %cI", "--", filepath.Base(path))
	if !ok || out == "" {
		return nil
	}
	sha, stamp, found := strings.Cut(out, " ")
	if !found {
		return nil
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return nil
	}
	return &Change{SHA: sha, Time: t.UTC()}
}

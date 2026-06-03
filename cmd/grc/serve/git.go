package serve

import "os/exec"

// newGitCommand creates an exec.Cmd for git in the given directory.
func newGitCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}

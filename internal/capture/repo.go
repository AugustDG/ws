package capture

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoInfo describes the git checkout a directory is in.
type RepoInfo struct {
	Top       string // checkout top level
	CommonDir string // shared .git dir; equal across a repo's worktrees
	Linked    bool   // true for a linked worktree, false for the main one
}

// RepoFunc looks up the checkout for dir. ok is false outside a repo.
type RepoFunc func(dir string) (info RepoInfo, ok bool)

// GitRepo asks git.
func GitRepo(dir string) (RepoInfo, bool) {
	out, err := exec.Command("git", "-C", dir, "rev-parse",
		"--show-toplevel", "--git-common-dir", "--git-dir").Output()
	if err != nil {
		return RepoInfo{}, false
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 3 {
		return RepoInfo{}, false
	}
	abs := func(p string) string {
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		return filepath.Clean(p)
	}
	common, gitDir := abs(lines[1]), abs(lines[2])
	return RepoInfo{Top: lines[0], CommonDir: common, Linked: common != gitDir}, true
}

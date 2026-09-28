package worktree

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGitAcquire(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	git(t, base, "init", "-q", "-b", "main", repo)
	git(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	git(t, repo, "worktree", "add", "-q", "-b", "a", filepath.Join(base, "wt-a"))
	git(t, repo, "worktree", "add", "-q", "-b", "b", filepath.Join(base, "wt-b"))

	leases, err := Git{}.Acquire(Request{Root: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 2 || leases[0].Name != "wt-a" || leases[1].Name != "wt-b" {
		t.Errorf("leases = %+v", leases)
	}

	leases, _ = Git{}.Acquire(Request{Root: repo, Count: 1})
	if len(leases) != 1 {
		t.Errorf("count 1 gave %d", len(leases))
	}
}

func TestGetUnknownSource(t *testing.T) {
	if _, err := Get("svn"); err == nil {
		t.Error("expected an error")
	}
}

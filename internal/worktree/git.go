package worktree

import (
	"path/filepath"
	"strings"
)

// Git uses the repo's existing linked worktrees (`git worktree list`). It
// creates and removes nothing.
type Git struct{}

func (Git) Acquire(req Request) ([]Lease, error) {
	out, err := run(req.Root, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var leases []Lease
	for i, block := range strings.Split(out, "\n\n") {
		if i == 0 {
			continue // the main worktree is the project root itself
		}
		path, ok := strings.CutPrefix(strings.SplitN(block, "\n", 2)[0], "worktree ")
		if !ok || strings.Contains(block, "\nprunable") {
			continue
		}
		leases = append(leases, Lease{Name: filepath.Base(path), Path: path})
		if req.Count > 0 && len(leases) == req.Count {
			break
		}
	}
	return leases, nil
}

func (Git) Release(string, []Lease) ([]Lease, error) { return nil, nil }

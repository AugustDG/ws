// Package worktree supplies the extra checkouts a project's `for_each:
// worktree` layouts expand over. Each backend implements Source and
// registers itself in sources.
package worktree

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Lease is one checkout handed out by a source. ID is empty for sources
// that don't track ownership.
type Lease struct {
	Name string `json:"name"`
	Path string `json:"path"`
	ID   string `json:"id,omitempty"`
}

// Request describes what a project wants from its source.
type Request struct {
	Root   string  // project root; the repo the worktrees belong to
	Count  int     // how many worktrees; 0 means the source's default
	Holder string  // label recorded by sources that track ownership
	Held   []Lease // leases from a previous start, reused when still valid
}

// Source hands out and takes back worktrees.
type Source interface {
	// Acquire returns the worktrees for req, reusing req.Held where it can
	// and releasing any it no longer needs. On error it still returns every
	// lease the caller should keep tracking.
	Acquire(req Request) ([]Lease, error)
	// Release gives leases back and returns the ones it couldn't, which the
	// caller should keep tracking. Sources that don't lease do nothing.
	Release(root string, leases []Lease) (kept []Lease, err error)
}

var sources = map[string]Source{
	"treehouse": Treehouse{},
	"git":       Git{},
}

// Get returns the source registered under name.
func Get(name string) (Source, error) {
	s, ok := sources[name]
	if !ok {
		return nil, fmt.Errorf("unknown worktree source %q (known: %s)", name, strings.Join(Names(), ", "))
	}
	return s, nil
}

func Names() []string {
	var out []string
	for n := range sources {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// SubdirOf returns root's path relative to its repo's top level, so a
// project rooted in a subdirectory opens the same subdirectory in every
// worktree. It returns "" when root is the top level or not in a repo.
func SubdirOf(root string) string {
	top, err := run(root, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(resolve(top), resolve(root))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return rel
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// run executes a command in dir and returns trimmed stdout. Stdin is closed
// so a prompting tool fails instead of hanging.
func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(lastLine(stderr.String()))
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", name, args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/project"
)

// target is what a command-line argument refers to.
type target struct {
	project project.Project
	// sessionOnly is set when the argument only names a running session
	// that has no project; it can be attached or stopped, not built.
	sessionOnly bool
}

// resolve finds what arg refers to, trying in order: a project name, a
// directory, a running session, then (when fuzzy) a zoxide match. An empty
// arg is the current directory.
func (a *app) resolve(arg string, fuzzy bool) (target, error) {
	if arg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return target{}, err
		}
		p, err := a.forDir(cwd)
		return target{project: p}, err
	}
	if _, err := os.Stat(a.cfg.ProjectFile(arg)); err == nil {
		p, err := a.cfg.Project(arg)
		return target{project: p}, err
	}
	if dir, ok := isDir(a.cfg.ExpandHome(arg)); ok {
		p, err := a.forDir(dir)
		return target{project: p}, err
	}
	if a.tmux.HasSession(arg) {
		return target{project: project.Project{Name: arg}, sessionOnly: true}, nil
	}
	if !fuzzy {
		return target{}, errNoMatch(arg)
	}
	if out, err := exec.Command("zoxide", "query", "--", arg).Output(); err == nil {
		if dir, ok := isDir(strings.TrimSpace(string(out))); ok {
			p, err := a.forDir(dir)
			return target{project: p}, err
		}
	}
	return target{}, errNoMatch(arg)
}

func errNoMatch(arg string) error {
	return fmt.Errorf("no project, directory or session matches %q", arg)
}

// forDir is cfg.ForDir, but a directory without a project file doesn't take
// a session name that already belongs to another directory. Two checkouts
// called "api" would otherwise share one session.
func (a *app) forDir(dir string) (project.Project, error) {
	p, err := a.cfg.ForDir(dir)
	if err != nil || p.File != "" || !a.nameTaken(p.Name, dir) {
		return p, err
	}
	p.Name = project.SessionName(filepath.Base(filepath.Dir(dir)) + "-" + filepath.Base(dir))
	return p, nil
}

// nameTaken reports whether a session or project named name is rooted
// somewhere other than dir.
func (a *app) nameTaken(name, dir string) bool {
	if sessions, err := a.tmux.Sessions(); err == nil {
		for _, s := range sessions {
			if s.Name == name && s.Path != dir {
				return true
			}
		}
	}
	if _, err := os.Stat(a.cfg.ProjectFile(name)); err == nil {
		return true
	}
	return false
}

func isDir(p string) (string, bool) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	return abs, err == nil && info.IsDir()
}

// completeTargets suggests project names and running sessions.
func completeTargets(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	a, err := newApp()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	seen := map[string]bool{}
	var names []string
	if projects, err := a.cfg.Projects(); err == nil {
		for _, p := range projects {
			seen[p.Name] = true
			names = append(names, p.Name)
		}
	}
	if sessions, err := a.tmux.Sessions(); err == nil {
		for _, s := range sessions {
			if !seen[s.Name] {
				names = append(names, s.Name)
			}
		}
	}
	return names, cobra.ShellCompDirectiveDefault
}

func completeLayouts(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	a, err := newApp()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	names, _ := a.cfg.Layouts()
	return names, cobra.ShellCompDirectiveNoFileComp
}

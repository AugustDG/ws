package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/worktree"
)

func checkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Validate every project and layout",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			failed := 0
			report := func(kind, name string, err error) {
				if err != nil {
					failed++
					fmt.Printf("✗ %s %s: %v\n", kind, name, err)
				} else {
					fmt.Printf("✓ %s %s\n", kind, name)
				}
			}

			layouts, err := a.cfg.Layouts()
			if err != nil {
				return err
			}
			for _, name := range layouts {
				report("layout", name, a.checkLayout(name))
			}
			projects, err := a.cfg.Projects()
			if err != nil {
				return err
			}
			for _, p := range projects {
				report("project", p.Name, a.checkProject(p))
			}
			if failed > 0 {
				return fmt.Errorf("%d problem(s)", failed)
			}
			return nil
		},
	}
}

// checkLayout expands a layout with two stand-in worktrees so for_each
// parts get exercised too.
func (a *app) checkLayout(name string) error {
	f, err := a.cfg.Layout(name)
	if err != nil {
		return err
	}
	_, err = layout.Expand(f.Windows, layout.Context{
		Project: "check", Root: "/check", HomeDir: a.cfg.Home,
		Worktrees: []layout.Worktree{{Name: "main", Path: "/check"}, {Name: "1", Path: "/check-1"}},
	})
	return err
}

func (a *app) checkProject(p project.Project) error {
	if _, ok := isDir(p.Root); !ok {
		return fmt.Errorf("root %s is not a directory", p.Root)
	}
	if p.Worktrees != nil {
		if _, err := worktree.Get(p.Worktrees.Source); err != nil {
			return err
		}
	}
	windows, err := a.cfg.Windows(p, "")
	if err != nil {
		return err
	}
	if len(windows) == 0 {
		return errors.New("no windows")
	}
	_, err = layout.Expand(windows, layout.Context{Project: p.Name, Root: p.Root, HomeDir: a.cfg.Home})
	return err
}

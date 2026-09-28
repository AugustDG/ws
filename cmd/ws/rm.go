package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func rmCmd() *cobra.Command {
	var isLayout, yes, force bool
	cmd := &cobra.Command{
		Use:     "rm <name>...",
		Aliases: []string{"remove"},
		Short:   "Remove projects (or with --layout, layouts) by name",
		Long: `Remove project files from projects/, or with --layout, layout files from
layouts/. A repo's own .ws.yaml is never touched.

Asks before removing unless -y is given, which scripts must pass. A layout
still used by a project or by default_layout is kept unless --force is
given. Removing a project whose session is running leaves the session
alone; ws stop still ends it and returns its worktrees.`,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeRemovable,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			kind := "project"
			if isLayout {
				kind = "layout"
				if !force {
					if err := a.checkLayoutsUnused(args); err != nil {
						return err
					}
				}
			}
			if !yes {
				if err := confirm(fmt.Sprintf("remove %s %s?", kind, strings.Join(args, ", "))); err != nil {
					return err
				}
			}

			var errs []error
			for _, name := range args {
				remove := a.cfg.RemoveProject
				if isLayout {
					remove = a.cfg.RemoveLayout
				}
				path, err := remove(name)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				fmt.Println("removed", a.cfg.AbbrevHome(path))
				if !isLayout && a.tmux.HasSession(name) {
					fmt.Fprintf(os.Stderr, "session %s is still running; ws stop %s ends it and returns its worktrees\n", name, name)
				}
			}
			return errors.Join(errs...)
		},
	}
	cmd.Flags().BoolVar(&isLayout, "layout", false, "remove layouts instead of projects")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask for confirmation")
	cmd.Flags().BoolVar(&force, "force", false, "remove layouts even if projects still use them")
	return cmd
}

// checkLayoutsUnused fails if any of the layouts is still referred to.
func (a *app) checkLayoutsUnused(names []string) error {
	var used []string
	for _, name := range names {
		users, err := a.cfg.LayoutUsers(name)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			used = append(used, fmt.Sprintf("%s (used by %s)", name, strings.Join(users, ", ")))
		}
	}
	if len(used) > 0 {
		return fmt.Errorf("layout still in use: %s; pass --force to remove anyway", strings.Join(used, "; "))
	}
	return nil
}

// confirm asks a yes/no question on the terminal. Without a terminal it
// fails, so scripts have to pass -y.
func confirm(question string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("not a terminal; pass -y to confirm")
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		return errors.New("cancelled")
	}
	return nil
}

// completeRemovable suggests project names, or layout names with --layout.
func completeRemovable(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	a, err := newApp()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	if layout, _ := cmd.Flags().GetBool("layout"); layout {
		names, _ := a.cfg.Layouts()
		return names, cobra.ShellCompDirectiveNoFileComp
	}
	projects, _ := a.cfg.Projects()
	var names []string
	for _, p := range projects {
		names = append(names, p.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

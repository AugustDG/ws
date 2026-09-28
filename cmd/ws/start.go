package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/workspace"
)

func startCmd() *cobra.Command {
	var layoutName string
	var detach bool
	cmd := &cobra.Command{
		Use:   "start [project|dir|session]",
		Short: "Start a workspace and attach to it",
		Long: `Start a workspace and attach to it.

If the session is already running, windows missing from it are added and
existing ones are left alone. With no argument, the current directory is
used.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			t, err := a.resolve(firstArg(args), true)
			if err != nil {
				return err
			}
			if !t.sessionOnly {
				if err := a.start(t, layoutName); err != nil {
					return err
				}
			}
			if detach {
				return nil
			}
			return a.tmux.Attach(t.project.Name)
		},
	}
	cmd.Flags().StringVarP(&layoutName, "layout", "l", "", "use this layout instead of the project's")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "don't attach after starting")
	_ = cmd.RegisterFlagCompletionFunc("layout", completeLayouts)
	return cmd
}

// start brings up t and reports what changed on stderr.
func (a *app) start(t target, layoutName string) error {
	res, err := a.mgr.Start(t.project, workspace.StartOptions{Layout: layoutName})
	if err != nil {
		return err
	}
	if err := a.installHooks(); err != nil {
		fmt.Fprintf(os.Stderr, "ws last won't follow session switches: %v\n", err)
	}
	switch {
	case res.Created:
		fmt.Fprintf(os.Stderr, "started %s\n", t.project.Name)
	case len(res.Added) > 0:
		fmt.Fprintf(os.Stderr, "added windows to %s: %s\n", t.project.Name, strings.Join(res.Added, ", "))
	}
	return nil
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

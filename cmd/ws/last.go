package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func lastCmd() *cobra.Command {
	var record string
	cmd := &cobra.Command{
		Use:   "last",
		Short: "Print the workspace you were last in",
		Long: `Print the workspace a tmux client was last in, as something ws start
accepts: its project name, or its directory when it has no project file.
The record survives tmux restarts, so a terminal can reopen it:

  ws start "$(ws last)"

ws start installs tmux hooks that keep the record current. Sessions ws
didn't start are never recorded.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if record != "" {
				return a.recordLast(record)
			}
			name, use, ok, err := a.mgr.State.Last()
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("no workspace recorded yet")
			}
			fmt.Println(a.lastTarget(name, use.Root))
			return nil
		},
	}
	cmd.Flags().StringVar(&record, "record", "", "internal: record this session, called by the tmux hooks")
	_ = cmd.Flags().MarkHidden("record")
	return cmd
}

// recordLast stamps session as used now, if ws started it, and reports
// the change over the link when this host was reached with ws ssh.
func (a *app) recordLast(session string) error {
	defer a.pushLink()
	s, ok, err := a.tmux.Session(session)
	if err != nil || !ok || !s.Managed {
		return err
	}
	return a.mgr.State.RecordUse(s.Name, s.Path)
}

// lastTarget is the argument ws start needs to reopen last. A session
// without a project file is only a name while it runs, so after that it's
// reopened by its directory.
func (a *app) lastTarget(name, root string) string {
	if _, err := os.Stat(a.cfg.ProjectFile(name)); err == nil || root == "" {
		return name
	}
	return root
}

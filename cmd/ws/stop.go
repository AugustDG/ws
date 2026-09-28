package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/workspace"
)

func stopCmd() *cobra.Command {
	var opts workspace.StopOptions
	var fromServer, all bool
	cmd := &cobra.Command{
		Use:   "stop [project|session]",
		Short: "Kill a workspace's session and return its worktrees",
		Long: `Kill a workspace's session, run its on_stop hook and return any
worktrees it leased. With no argument, the current session is stopped.

--all stops every session ws started and returns worktrees still recorded
for sessions that are already gone. Sessions started some other way are
left alone.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if all {
				if len(args) > 0 {
					return fmt.Errorf("--all takes no argument")
				}
				return a.stopAll(opts)
			}
			name, err := a.sessionName(firstArg(args))
			if err != nil {
				return err
			}
			// Killing our own session would kill this process before the
			// worktrees are returned, so hand the job to the tmux server.
			if current, _ := a.tmux.CurrentSession(); current == name && !fromServer {
				return a.stopFromServer(name, opts)
			}
			return a.mgr.Stop(name, opts)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "stop every workspace ws started")
	cmd.Flags().BoolVar(&opts.KeepWorktrees, "keep-worktrees", false, "keep leased worktrees for the next start")
	cmd.Flags().BoolVar(&fromServer, "from-server", false, "internal: running via tmux run-shell")
	_ = cmd.Flags().MarkHidden("from-server")
	return cmd
}

// stopAll stops every workspace. The current session goes last, through
// the tmux server, since killing it ends this process.
func (a *app) stopAll(opts workspace.StopOptions) error {
	names, err := a.mgr.Workspaces()
	if err != nil {
		return err
	}
	current, _ := a.tmux.CurrentSession()
	var errs []error
	for _, name := range names {
		if name == current {
			continue
		}
		fmt.Fprintf(os.Stderr, "stopping %s\n", name)
		if err := a.mgr.Stop(name, opts); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	if slices.Contains(names, current) {
		fmt.Fprintf(os.Stderr, "stopping %s\n", current)
		errs = append(errs, a.stopFromServer(current, opts))
	}
	return errors.Join(errs...)
}

// sessionName maps an argument to the session it stops. It resolves like
// start without zoxide guessing, and falls back to the argument itself so a
// session that's already gone can still release its worktrees.
func (a *app) sessionName(arg string) (string, error) {
	if arg == "" {
		return a.tmux.CurrentSession()
	}
	t, err := a.resolve(arg, false)
	if err != nil {
		return arg, nil
	}
	return t.project.Name, nil
}

// stopFromServer re-runs this stop through `tmux run-shell -b`. The job
// gets the tmux server's environment, so the dirs and socket this process
// uses are passed explicitly.
func (a *app) stopFromServer(name string, opts workspace.StopOptions) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	argv := []string{"env",
		"WS_CONFIG_DIR=" + a.cfg.Dir,
		"WS_STATE_DIR=" + a.mgr.State.Dir,
		"WS_TMUX_SOCKET=" + a.tmux.Socket,
		exe, "stop", "--from-server"}
	if opts.KeepWorktrees {
		argv = append(argv, "--keep-worktrees")
	}
	argv = append(argv, "--", name)
	log := filepath.Join(a.mgr.State.Dir, "stop.log")
	script := fmt.Sprintf("mkdir -p %s && %s >>%s 2>&1", quote(a.mgr.State.Dir), quoteAll(argv), quote(log))
	// run-shell expands #{formats}; ## is a literal #.
	_, err = a.tmux.Run("run-shell", "-b", strings.ReplaceAll(script, "#", "##"))
	return err
}

func quoteAll(args []string) string {
	q := make([]string, len(args))
	for i, s := range args {
		q[i] = quote(s)
	}
	return strings.Join(q, " ")
}

// quote single-quotes s for sh.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

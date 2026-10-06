package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AugustDG/ws/internal/tmux"
	"github.com/AugustDG/ws/internal/workspace"
)

// Jobs ws hands to the tmux server run with the server's environment, not
// this process's, so the config dir, state dir and socket in use are passed
// explicitly. Their output goes to a log in the state dir.

// stopFromServer re-runs this stop through `tmux run-shell -b`.
func (a *app) stopFromServer(name string, opts workspace.StopOptions) error {
	args := []string{"stop", "--from-server"}
	if opts.KeepWorktrees {
		args = append(args, "--keep-worktrees")
	}
	script, err := a.serverScript("stop.log", append(args, "--", name), "")
	if err != nil {
		return err
	}
	_, err = a.tmux.Run("run-shell", "-b", script)
	return err
}

// recordHookIndex is the entry ws owns in each hook it sets, high enough to
// stay clear of hooks set in tmux.conf.
const recordHookIndex = 77

// installHooks has the tmux server record the workspace each client
// switches or attaches to, for ws last, and on a host reached with ws ssh,
// report its workspaces whenever they change. Setting them again is
// harmless.
func (a *app) installHooks() error {
	script, err := a.serverScript("last.log", []string{"last"}, "--record=#{q:session_name}")
	if err != nil {
		return err
	}
	cmd := "run-shell -b " + tmux.Quote(script)
	for _, hook := range []string{"client-session-changed", "client-attached"} {
		if err := a.tmux.SetHook(hook, recordHookIndex, cmd); err != nil {
			return err
		}
	}
	// On a host reached with ws ssh, new and closed sessions are reported
	// over the link too; ws last --record reports on the others.
	push, err := a.serverScript("push.log", []string{"ssh", "push"}, "")
	if err != nil {
		return err
	}
	cmd = "run-shell -b " + tmux.Quote(push)
	for _, hook := range []string{"session-created", "session-closed"} {
		if err := a.tmux.SetHook(hook, recordHookIndex, cmd); err != nil {
			return err
		}
	}
	return nil
}

// serverScript is a run-shell script that runs ws with args, then with
// format appended as is. run-shell expands #{formats} in the script, so
// every # outside format is doubled.
func (a *app) serverScript(logName string, args []string, format string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	argv := append([]string{"env",
		"WS_CONFIG_DIR=" + a.cfg.Dir,
		"WS_STATE_DIR=" + a.mgr.State.Dir,
		"WS_TMUX_SOCKET=" + a.tmux.Socket,
		exe}, args...)
	dir := a.mgr.State.Dir
	script := fmt.Sprintf("mkdir -p %s && %s", quote(dir), quoteAll(argv))
	script = strings.ReplaceAll(script, "#", "##")
	if format != "" {
		script += " " + format
	}
	return script + " >>" + strings.ReplaceAll(quote(filepath.Join(dir, logName)), "#", "##") + " 2>&1", nil
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

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/remote"
	"github.com/AugustDG/ws/internal/tmux"
)

func sshCmd() *cobra.Command {
	var returnTo string
	var fromTmux bool
	cmd := &cobra.Command{
		Use:   "ssh HOST [TARGET]",
		Short: "Attach to a workspace on another host",
		Long: `Attach this terminal to a tmux session on HOST over ssh.

HOST is anything ssh accepts, including ~/.ssh/config aliases. When ws is
installed on the host, TARGET goes to its ws start; without a TARGET the
host's last workspace is reopened. Without ws, TARGET names a plain tmux
session ("main" by default).

Inside tmux, the local client hands the terminal over to ssh, so there's
no nesting and the host's tmux sees every key. Detaching on the host
brings back the local session you left. The remote session keeps running
after a detach or a dropped connection, and a dropped connection is
retried until it's back or you press ctrl-c.`,
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn := &remote.Conn{Host: args[0]}
			if len(args) > 1 {
				conn.Target = args[1]
			}
			if err := conn.Validate(); err != nil {
				return err
			}
			tc := tmux.New()
			switch {
			case fromTmux:
				return returnToTmux(tc, conn, returnTo)
			case tmux.Inside():
				return handOff(tc, args)
			default:
				return conn.Run()
			}
		},
	}
	cmd.Flags().BoolVar(&fromTmux, "from-tmux", false, "internal: running in place of a detached tmux client")
	cmd.Flags().StringVar(&returnTo, "return-to", "", "internal: session to reattach afterwards")
	_ = cmd.Flags().MarkHidden("from-tmux")
	_ = cmd.Flags().MarkHidden("return-to")
	return cmd
}

// handOff detaches this tmux client and has it run `ws ssh` in its place,
// so the host's tmux isn't nested inside the local one.
func handOff(tc *tmux.Client, args []string) error {
	session, err := tc.CurrentSession()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	var argv []string
	if tc.Socket != "" {
		argv = append(argv, "env", "WS_TMUX_SOCKET="+tc.Socket)
	}
	argv = append(argv, exe, "ssh", "--from-tmux", "--return-to", session, "--")
	argv = append(argv, args...)
	_, err = tc.Run("detach-client", "-E", "exec "+quoteAll(argv))
	return err
}

// returnToTmux runs the connection, then reattaches the local session it
// was started from, or the most recent one if that's gone. An error stays
// on screen until enter, since reattaching would hide it.
func returnToTmux(tc *tmux.Client, conn *remote.Conn, session string) error {
	if err := conn.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ws ssh: %v\npress enter to return to tmux", err)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	// The replaced client isn't inside tmux, whatever its environment says.
	os.Unsetenv("TMUX")
	if !tc.HasSession(session) {
		session = ""
	}
	return tc.Attach(session)
}

func completeHosts(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return remote.Hosts(filepath.Join(home, ".ssh", "config")), cobra.ShellCompDirectiveNoFileComp
}

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/discover"
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
retried until it's back or you press ctrl-c.

While connected, the host's picker also lists this machine's sessions and
projects ("local session") and the other hosts you use. Picking one
detaches from the host and opens it here.

ws ssh setup HOST installs tmux and ws on HOST and copies your config.`,
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			host, target := args[0], ""
			if len(args) > 1 {
				target = args[1]
			}
			a, err := newApp()
			if err != nil {
				return err
			}
			if fromTmux {
				return a.returnToTmux(host, target, returnTo)
			}
			return a.connect(host, target)
		},
	}
	cmd.Flags().BoolVar(&fromTmux, "from-tmux", false, "internal: running in place of a detached tmux client")
	cmd.Flags().StringVar(&returnTo, "return-to", "", "internal: session to reattach afterwards")
	_ = cmd.Flags().MarkHidden("from-tmux")
	_ = cmd.Flags().MarkHidden("return-to")
	cmd.AddCommand(sshSetupCmd())
	return cmd
}

// connect attaches this terminal to target on host: through a handoff
// inside tmux, else directly, then opens whatever was picked from this
// machine's items on the host.
func (a *app) connect(host, target string) error {
	if err := (&remote.Conn{Host: host}).Validate(); err != nil {
		return err
	}
	if tmux.Inside() {
		args := []string{host}
		if target != "" {
			args = append(args, target)
		}
		return handOff(a.tmux, args)
	}
	it, err := a.visit(host, target)
	if err != nil || it == nil {
		return err
	}
	return a.open(*it)
}

// visit connects to target on host, recording each session that ends
// cleanly for the picker. Picking another host in the host's picker moves
// there. Picking one of this machine's items ends the visit and returns
// it; detaching returns nil.
func (a *app) visit(host, target string) (*discover.Item, error) {
	for {
		conn := &remote.Conn{Host: host, Target: target}
		link := a.listenLink(host)
		if link != nil {
			conn.LocalSocket = link.Path
		}
		err := conn.Run()
		var chosen discover.Item
		var picked bool
		if link != nil {
			chosen, picked = link.Chosen()
			link.Close()
		}
		if err != nil {
			return nil, err
		}
		if err := a.mgr.State.RecordRemote(host, target); err != nil {
			fmt.Fprintf(os.Stderr, "ws ssh: not recorded for the picker: %v\n", err)
		}
		if !picked {
			return nil, nil
		}
		if chosen.Kind != discover.Remote {
			return &chosen, nil
		}
		host, target = chosen.Host, chosen.Target
	}
}

// listenLink serves this machine's items to the host's picker. It returns
// nil when it can't; the connection works without it.
func (a *app) listenLink(host string) *remote.Link {
	dir := filepath.Join(a.mgr.State.Dir, "ssh")
	path := filepath.Join(dir, fmt.Sprintf("link-%d.sock", os.Getpid()))
	if len(path) > 100 { // past the Unix socket path limit
		path = filepath.Join(os.TempDir(), fmt.Sprintf("ws-link-%d.sock", os.Getpid()))
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	link, err := remote.Listen(path, func() []discover.Item {
		return slices.DeleteFunc(a.items(), func(it discover.Item) bool {
			return it.Kind == discover.Remote && it.Host == host
		})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ws ssh: the host's picker won't list this machine: %v\n", err)
		return nil
	}
	return link
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

// returnToTmux runs the visit in place of a detached tmux client, then
// opens what was picked on the host, or reattaches the local session it
// was started from (the most recent one if that's gone). An error stays
// on screen until enter, since reattaching would hide it.
func (a *app) returnToTmux(host, target, session string) error {
	// The replaced client isn't inside tmux, whatever its environment says.
	os.Unsetenv("TMUX")
	it, err := a.visit(host, target)
	if err == nil && it != nil {
		if err = a.open(*it); err == nil {
			return nil
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ws ssh: %v\npress enter to return to tmux", err)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	if !a.tmux.HasSession(session) {
		session = ""
	}
	return a.tmux.Attach(session)
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

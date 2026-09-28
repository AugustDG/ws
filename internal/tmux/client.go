// Package tmux is a thin wrapper over the tmux CLI. Everything ws does to
// tmux goes through Client so it can target a separate server in tests.
package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Client runs tmux commands. Socket, when set, selects a named server (-L).
type Client struct {
	Bin    string
	Socket string
}

// New returns a client for the default server, or the one named by
// $WS_TMUX_SOCKET.
func New() *Client { return &Client{Bin: "tmux", Socket: os.Getenv("WS_TMUX_SOCKET")} }

func (c *Client) args(args []string) []string {
	if c.Socket == "" {
		return args
	}
	return append([]string{"-L", c.Socket}, args...)
}

// Run executes tmux and returns trimmed stdout.
func (c *Client) Run(args ...string) (string, error) {
	cmd := exec.Command(c.Bin, c.args(args)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("tmux %s: %s", args[0], msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// Lines runs tmux and splits stdout into lines, each split on tabs. A
// missing server counts as no output.
func (c *Client) Lines(args ...string) ([][]string, error) {
	out, err := c.Run(args...)
	if err != nil {
		if isNoServer(err) {
			return nil, nil
		}
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var rows [][]string
	for _, line := range strings.Split(out, "\n") {
		rows = append(rows, strings.Split(line, "\t"))
	}
	return rows, nil
}

func isNoServer(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no server running") ||
		strings.Contains(msg, "error connecting to") ||
		strings.Contains(msg, "No such file or directory")
}

// Inside reports whether ws runs inside a tmux client.
func Inside() bool { return os.Getenv("TMUX") != "" }

// Exact turns a session name into a target that doesn't prefix-match.
func Exact(session string) string { return "=" + session }

// HasSession reports whether a session with this exact name exists.
func (c *Client) HasSession(name string) bool {
	_, err := c.Run("has-session", "-t", Exact(name))
	return err == nil
}

// Session is a running tmux session.
type Session struct {
	Name     string
	Path     string
	Windows  string
	Attached bool
	Managed  bool // created by ws, see ManagedOption
}

// ManagedOption is the session user option ws sets on sessions it creates,
// so they can be told apart from ones started some other way.
const ManagedOption = "@ws"

// MarkManaged sets ManagedOption on the session that target belongs to.
func (c *Client) MarkManaged(target string) error {
	_, err := c.Run("set-option", "-t", target, ManagedOption, "1")
	return err
}

func (c *Client) Sessions() ([]Session, error) {
	rows, err := c.Lines("list-sessions", "-F",
		"#{session_name}\t#{session_path}\t#{session_windows}\t#{session_attached}\t#{"+ManagedOption+"}")
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, r := range rows {
		if len(r) < 5 {
			continue
		}
		out = append(out, Session{Name: r[0], Path: r[1], Windows: r[2], Attached: r[3] != "0", Managed: r[4] == "1"})
	}
	return out, nil
}

// WindowNames lists the window names of a session.
func (c *Client) WindowNames(session string) ([]string, error) {
	rows, err := c.Lines("list-windows", "-t", Exact(session), "-F", "#{window_name}")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		out = append(out, r[0])
	}
	return out, nil
}

// CurrentSession is the session of the client ws runs in.
func (c *Client) CurrentSession() (string, error) {
	if !Inside() {
		return "", errors.New("not inside tmux")
	}
	return c.Run("display-message", "-p", "#{session_name}")
}

// ClientSize returns the size of the current tmux client, if any.
func (c *Client) ClientSize() (w, h int, ok bool) {
	if !Inside() {
		return 0, 0, false
	}
	out, err := c.Run("display-message", "-p", "#{client_width} #{client_height}")
	if err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(out, "%d %d", &w, &h); err != nil {
		return 0, 0, false
	}
	return w, h, true
}

// Attach switches the current client to the session, or replaces this
// process with `tmux attach` when ws runs outside tmux.
func (c *Client) Attach(session string) error {
	if Inside() {
		_, err := c.Run("switch-client", "-t", Exact(session))
		return err
	}
	bin, err := exec.LookPath(c.Bin)
	if err != nil {
		return err
	}
	argv := append([]string{c.Bin}, c.args([]string{"attach-session", "-t", Exact(session)})...)
	return syscall.Exec(bin, argv, os.Environ())
}

func (c *Client) KillSession(name string) error {
	_, err := c.Run("kill-session", "-t", Exact(name))
	return err
}

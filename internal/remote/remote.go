// Package remote attaches to tmux on another host over ssh, reconnecting
// when the connection drops.
package remote

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"github.com/AugustDG/ws/internal/tmux"
)

// DefaultSession is the tmux session used on hosts without ws when no
// target is given.
const DefaultSession = "main"

// Script is the shell script run on the host. With ws installed it runs
// `ws start TARGET`, or reopens the host's last workspace when there's no
// target. Without ws it attaches to (or creates) the tmux session TARGET.
// ~/.local/bin is added to PATH because ssh runs commands in a
// non-login shell, which often leaves it out. A non-empty link is the
// forwarded socket, recorded in the host's state dir for its picker.
func Script(target, link string) string {
	start := `"$(ws last 2>/dev/null)"`
	session := DefaultSession
	if target != "" {
		start = tmux.Quote(target)
		session = target
	}
	lines := []string{`PATH="$HOME/.local/bin:$PATH"; export PATH`}
	if link != "" {
		lines = append(lines,
			`d="${WS_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/ws}"`,
			`mkdir -p "$d" && printf %s `+tmux.Quote(link)+` > "$d/`+LinkFile+`"`)
	}
	return strings.Join(append(lines,
		`if command -v ws >/dev/null 2>&1; then exec ws start `+start+`; fi`,
		`if command -v tmux >/dev/null 2>&1; then exec tmux new-session -A -s `+tmux.Quote(session)+`; fi`,
		`echo "ws ssh: neither ws nor tmux is installed on this host; run ws ssh setup for it" >&2`,
		`exit 127`,
	), "\n")
}

// Conn is one `ws ssh` connection.
type Conn struct {
	Host   string
	Target string

	SSH string // ssh binary; "ssh" when empty
	Log io.Writer

	// LocalSocket, when set, is forwarded to the host as the link (see
	// Listen), at a new path for each connection attempt.
	LocalSocket string
	link        string

	// Live is how long a connection must have lasted to count as
	// established. Only an established connection is retried when it
	// drops, so a typo'd host or failed login fails at once.
	Live time.Duration
	// Backoff is the first retry delay. It doubles up to MaxBackoff.
	Backoff, MaxBackoff time.Duration
}

// Validate rejects hosts ssh would read as an option.
func (c *Conn) Validate() error {
	if c.Host == "" || strings.HasPrefix(c.Host, "-") {
		return fmt.Errorf("invalid host %q", c.Host)
	}
	return nil
}

// Args are the arguments to ssh. The keepalives make a dead connection
// exit within ~45s instead of hanging. The script runs under sh so it
// doesn't depend on the host's login shell.
func (c *Conn) Args() []string {
	args := []string{
		"-t",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
	}
	if c.link != "" {
		args = append(args, "-R", c.link+":"+c.LocalSocket)
	}
	return append(args, c.Host, "exec sh -c "+tmux.Quote(Script(c.Target, c.link)))
}

// sshDropped is the exit status ssh uses for its own errors, including a
// lost connection.
const sshDropped = 255

// Run connects and blocks until the remote client exits. When an
// established connection drops it reconnects with backoff until it gets
// back in or is interrupted.
func (c *Conn) Run() error {
	if err := c.Validate(); err != nil {
		return err
	}
	c.defaults()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)

	established := false
	attempt := 0
	for {
		began := time.Now()
		err := c.ssh()
		if exitCode(err) != sshDropped || interrupted(interrupt) {
			return err
		}
		resetTerminal(c.Log)
		if time.Since(began) >= c.Live {
			established, attempt = true, 0
		}
		if !established {
			return err
		}
		delay := min(c.Backoff<<attempt, c.MaxBackoff)
		attempt++
		fmt.Fprintf(c.Log, "ws: connection to %s lost, reconnecting in %s (ctrl-c to stop)\r\n", c.Host, delay)
		select {
		case <-interrupt:
			return err
		case <-time.After(delay):
		}
	}
}

func (c *Conn) defaults() {
	if c.SSH == "" {
		c.SSH = "ssh"
	}
	if c.Log == nil {
		c.Log = os.Stderr
	}
	if c.Live == 0 {
		c.Live = 10 * time.Second
	}
	if c.Backoff == 0 {
		c.Backoff = time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 15 * time.Second
	}
}

func (c *Conn) ssh() error {
	if c.LocalSocket != "" {
		c.link = newLinkPath()
	}
	cmd := exec.Command(c.SSH, c.Args()...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return &ExitError{Host: c.Host, Code: exit.ExitCode()}
	}
	return err
}

// ExitError is a non-zero exit from ssh or the remote command.
type ExitError struct {
	Host string
	Code int
}

func (e *ExitError) Error() string {
	if e.Code == sshDropped {
		return fmt.Sprintf("ssh to %s failed", e.Host)
	}
	return fmt.Sprintf("%s: remote command exited with status %d", e.Host, e.Code)
}

func exitCode(err error) int {
	var e *ExitError
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// interrupted drains a ctrl-c that arrived while ssh ran. ssh gets the
// same signal, so ctrl-c while connecting stops instead of retrying.
func interrupted(ch chan os.Signal) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// resetTerminal undoes modes the remote tmux had set when its connection
// died without cleaning up: alternate screen, hidden cursor, mouse
// reporting and bracketed paste.
func resetTerminal(w io.Writer) {
	fmt.Fprint(w, "\x1b[?1049l\x1b[?25h\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\r\n")
}

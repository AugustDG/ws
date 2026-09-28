package capture

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AugustDG/ws/internal/shellinit"
	"github.com/AugustDG/ws/internal/tmux"
)

func TestProgramOf(t *testing.T) {
	tests := map[string]string{
		"po":                         "po",
		"bun run bench":              "bun",
		`PATH="$X/bin:$PATH" pi`:     "pi",
		`FOO="a b" BAR=1 sleep 3`:    "sleep",
		`env PATH=/x pi --resume`:    "pi",
		`'my tool' --flag`:           `'my tool'`,
		`A=1`:                        "",
		`./run.sh "arg with spaces"`: "./run.sh",
	}
	for in, want := range tests {
		if got := programOf(in); got != want {
			t.Errorf("programOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestShellHooks runs each shell with the real shell-init snippet on a
// private tmux server and checks capture replays commands as typed.
func TestShellHooks(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	shells := map[shellinit.Shell]string{
		shellinit.Zsh:  "zsh -f",
		shellinit.Bash: "bash --norc --noprofile",
	}
	for shell, start := range shells {
		t.Run(string(shell), func(t *testing.T) {
			bin := strings.Fields(start)[0]
			if _, err := exec.LookPath(bin); err != nil {
				t.Skipf("%s not installed", bin)
			}
			if shell == shellinit.Bash && !bashHasPS0() {
				t.Skip("bash older than 4.4 has no PS0; the hook is a no-op there")
			}
			testShellHook(t, shell, start)
		})
	}
}

func bashHasPS0() bool {
	out, err := exec.Command("bash", "-c", `(( BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 4) )) && echo yes`).Output()
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}

func testShellHook(t *testing.T, shell shellinit.Shell, start string) {
	t.Setenv("TMUX", "")
	c := &tmux.Client{Bin: "tmux", Socket: fmt.Sprintf("ws-hook-%s-%d", shell, os.Getpid())}
	t.Cleanup(func() { c.Run("kill-server") })

	script, err := shellinit.Script(shell)
	if err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Run("new-session", "-d", "-s", "hook", "-x", "160", "-y", "40", start); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run("split-window", "-t", "=hook:", start); err != nil {
		t.Fatal(err)
	}
	type_ := func(pane, line string) {
		t.Helper()
		if _, err := c.Run("send-keys", "-t", "=hook:."+pane, "-l", line); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Run("send-keys", "-t", "=hook:."+pane, "Enter"); err != nil {
			t.Fatal(err)
		}
	}
	for _, pane := range []string{"0", "1"} {
		type_(pane, "source "+hook)
		type_(pane, "alias zz='env FOO=1 sleep 302'")
	}
	if shell == shellinit.Bash {
		type_("0", "shopt -s expand_aliases")
	}
	type_("0", "zz")
	type_("1", `FOO="a b" sleep 303`)

	want := map[Commands][]string{
		WithArgs:  {"zz", `FOO="a b" sleep 303`},
		ExecsOnly: {"zz", "sleep"},
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var failures []string
		for mode, cmds := range want {
			windows, _, err := FromSession(c, "hook", mode)
			if err != nil {
				t.Fatal(err)
			}
			for i, p := range windows[0].Panes {
				if got := strings.Join(p.Cmds, ""); got != cmds[i] {
					failures = append(failures, fmt.Sprintf("mode %d pane %d: got %q, want %q", mode, i, got, cmds[i]))
				}
			}
		}
		if len(failures) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(strings.Join(failures, "\n"))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

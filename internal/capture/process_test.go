package capture

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/AugustDG/ws/internal/tmux"
)

func fakePath(name string) (string, error) {
	switch name {
	case "claude", "bun", "tool":
		return "/usr/local/bin/" + name, nil
	}
	return "", errors.New("not found")
}

func TestTidy(t *testing.T) {
	tests := map[string]string{
		"claude":                          "claude",
		"bun run bench":                   "bun run bench",
		"/usr/local/bin/claude --resume":  "claude --resume",
		"/opt/other/claude":               "/opt/other/claude", // a different claude than PATH's
		"node /usr/local/bin/tool --flag": "tool --flag",
		"node /srv/app/server.js":         "node /srv/app/server.js",
		"-zsh":                            "",
		"zsh":                             "",
		"bash -l":                         "",
		"/bin/zsh -l":                     "",
		"bash /usr/local/bin/tool":        "tool",
		"bash ./deploy.sh":                "bash ./deploy.sh",
		"pi ":                             "pi",
		"":                                "",
	}
	for in, want := range tests {
		if got := Tidy(in, fakePath); got != want {
			t.Errorf("Tidy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSharedCommandsKeepsCommonOnes(t *testing.T) {
	ws := gridWindows(t)
	// Every top pane runs nvim; the bottoms differ.
	for i := range ws[0].Panes {
		if i%2 == 0 {
			ws[0].Panes[i].Cmds = []string{"nvim"}
		}
	}
	ws[0].Panes[3].Cmds = []string{"bun dev"}
	res := Templatize(ws, Options{Root: home, Home: home, Generalize: true, Repo: fakeRepo})

	tmpl := res.Windows[0].Columns[0]
	if tmpl.ForEach == "" {
		t.Fatal("should still generalize")
	}
	if got := tmpl.Rows[0].Cmd; len(got) != 1 || got[0] != "nvim" {
		t.Errorf("shared command lost: %v", got)
	}
	if len(tmpl.Rows[1].Cmd) != 0 {
		t.Errorf("differing command kept: %v", tmpl.Rows[1].Cmd)
	}
	if len(res.Notes) != 2 || !strings.Contains(res.Notes[1], "bun dev in ~/.treehouse/pool/1/viber") {
		t.Errorf("notes = %q", res.Notes)
	}
}

func TestFromSessionCommands(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("TMUX", "")
	c := &tmux.Client{Bin: "tmux", Socket: fmt.Sprintf("ws-capture-test-%d", os.Getpid())}
	t.Cleanup(func() { c.Run("kill-server") })

	// Pane 0: an idle shell. Pane 1: sleep typed into a shell. Pane 2: sleep
	// as the pane's own process.
	shell := "sh"
	if _, err := c.Run("new-session", "-d", "-s", "cap", "-x", "120", "-y", "30", shell); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run("split-window", "-t", "=cap:", shell); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run("send-keys", "-t", "=cap:.1", "sleep 300", "Enter"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Run("split-window", "-t", "=cap:", "sleep 301"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)

	want := map[Commands][]string{
		NoCommands: {"", "", ""},
		ExecsOnly:  {"", "sleep", "sleep"},
		WithArgs:   {"", "sleep 300", "sleep 301"},
	}
	for mode, cmds := range want {
		windows, _, err := FromSession(c, "cap", mode)
		if err != nil {
			t.Fatal(err)
		}
		panes := windows[0].Panes
		if len(panes) != 3 {
			t.Fatalf("mode %d: %d panes", mode, len(panes))
		}
		for i, p := range panes {
			if got := strings.Join(p.Cmds, ""); got != cmds[i] {
				t.Errorf("mode %d pane %d: got %q, want %q", mode, i, got, cmds[i])
			}
		}
	}
}

func TestCommonDir(t *testing.T) {
	ws := []Window{{Panes: []Pane{{Dir: "/home/me/a/x"}, {Dir: "/home/me/b"}}}, {Panes: []Pane{{Dir: "/home/me/a"}}}}
	if got := commonDir(ws); got != "/home/me" {
		t.Errorf("got %q", got)
	}
	if got := commonDir([]Window{{Panes: []Pane{{Dir: "/a"}, {Dir: "/b"}}}}); got != "/" {
		t.Errorf("disjoint dirs: got %q", got)
	}
}

func TestKeepCommandsOnlyGeneralizesUniformRepeats(t *testing.T) {
	opts := Options{Root: home, Home: home, Generalize: true, KeepCommands: true, Repo: fakeRepo}

	differing := gridWindows(t)
	differing[0].Panes[0].Cmds = []string{"claude"}
	res := Templatize(differing, opts)
	if res.Worktrees != nil || res.Windows[0].Columns[0].ForEach != "" {
		t.Error("differing commands should keep the window concrete")
	}
	if got := res.Windows[0].Columns[0].Rows[0].Cmd; len(got) != 1 || got[0] != "claude" {
		t.Errorf("command lost: %v", got)
	}

	uniform := gridWindows(t)
	for i := range uniform[0].Panes {
		uniform[0].Panes[i].Cmds = []string{"nvim ."}
	}
	res = Templatize(uniform, opts)
	tmpl := res.Windows[0].Columns[0]
	if tmpl.ForEach == "" || tmpl.Rows[0].Cmd[0] != "nvim ." {
		t.Errorf("uniform commands should still generalize: %+v", tmpl)
	}
}

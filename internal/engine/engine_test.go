package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/tmux"
)

// newServer starts tests against a private tmux server that's torn down
// afterwards.
func newServer(t *testing.T) *tmux.Client {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	t.Setenv("TMUX", "")
	c := &tmux.Client{Bin: "tmux", Socket: fmt.Sprintf("ws-test-%d", os.Getpid())}
	t.Cleanup(func() { c.Run("kill-server") })
	return c
}

func expand(t *testing.T, src string, ctx layout.Context) []layout.Resolved {
	t.Helper()
	var f layout.File
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	r, err := layout.Expand(f.Windows, ctx)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type paneInfo struct {
	left, top, width, height int
	path                     string
}

func panes(t *testing.T, c *tmux.Client, target string) []paneInfo {
	t.Helper()
	rows, err := c.Lines("list-panes", "-t", target, "-F", "#{pane_left}\t#{pane_top}\t#{pane_width}\t#{pane_height}\t#{pane_start_path}")
	if err != nil {
		t.Fatal(err)
	}
	var out []paneInfo
	for _, r := range rows {
		var p paneInfo
		fmt.Sscan(r[0], &p.left)
		fmt.Sscan(r[1], &p.top)
		fmt.Sscan(r[2], &p.width)
		fmt.Sscan(r[3], &p.height)
		p.path = r[4]
		out = append(out, p)
	}
	return out
}

func TestUpBuildsWorktreeGrid(t *testing.T) {
	c := newServer(t)
	root := t.TempDir()
	wt1, wt2 := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(root, "web"), 0o755)

	windows := expand(t, `
windows:
  - name: code
    columns:
      - for_each: worktree
        rows:
          - size: 70%
          - {}
  - name: misc
    layout: even-horizontal
    panes: [{dir: web}, {}]
`, layout.Context{Project: "grid", Root: root, Worktrees: []layout.Worktree{
		{Name: "main", Path: root}, {Name: "1", Path: wt1}, {Name: "2", Path: wt2},
	}})

	e := &Engine{Tmux: c, Width: 200, Height: 50}
	res, err := e.Up(Session{Name: "grid", Windows: windows})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created {
		t.Error("expected a new session")
	}

	names, _ := c.WindowNames("grid")
	if !slices.Equal(names, []string{"code", "misc"}) {
		t.Errorf("windows = %v", names)
	}

	code := panes(t, c, "=grid:code")
	if len(code) != 6 {
		t.Fatalf("code has %d panes", len(code))
	}
	// Panes come back top-to-bottom within each column, left to right.
	wantDirs := []string{root, root, wt1, wt1, wt2, wt2}
	for i, p := range code {
		if resolve(p.path) != resolve(wantDirs[i]) {
			t.Errorf("pane %d path %s, want %s", i, p.path, wantDirs[i])
		}
	}
	for col := 0; col < 3; col++ {
		top, bottom := code[col*2], code[col*2+1]
		if w := top.width; w < 62 || w > 68 {
			t.Errorf("column %d width %d, want about a third of 200", col, w)
		}
		share := float64(top.height) / float64(top.height+bottom.height+1)
		if share < 0.66 || share > 0.74 {
			t.Errorf("column %d top share %.2f, want about 0.70", col, share)
		}
	}

	misc := panes(t, c, "=grid:misc")
	if len(misc) != 2 || resolve(misc[0].path) != resolve(filepath.Join(root, "web")) {
		t.Errorf("misc = %+v", misc)
	}
}

func TestUpAddsMissingWindowsOnly(t *testing.T) {
	c := newServer(t)
	root := t.TempDir()
	ctx := layout.Context{Project: "re", Root: root}
	e := &Engine{Tmux: c, Width: 120, Height: 40}

	if _, err := e.Up(Session{Name: "re", Windows: expand(t, `windows: [{name: a, columns: [{}, {}]}]`, ctx)}); err != nil {
		t.Fatal(err)
	}
	res, err := e.Up(Session{Name: "re", Windows: expand(t, `windows: [{name: a}, {name: b}]`, ctx)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || !slices.Equal(res.Added, []string{"b"}) {
		t.Errorf("result = %+v", res)
	}
	if n := len(panes(t, c, "=re:a")); n != 2 {
		t.Errorf("existing window a was changed: %d panes", n)
	}
}

func TestUpSendsCommands(t *testing.T) {
	c := newServer(t)
	root := t.TempDir()
	out := filepath.Join(root, "ran")
	windows := expand(t, fmt.Sprintf(`windows: [{name: a, cmd: "echo {project} > %s"}]`, out),
		layout.Context{Project: "cmds", Root: root})
	e := &Engine{Tmux: c, Width: 80, Height: 24}
	if _, err := e.Up(Session{Name: "cmds", Windows: windows}); err != nil {
		t.Fatal(err)
	}
	// The shell may take a moment to start and run it.
	if _, err := c.Run("run-shell", fmt.Sprintf("for i in $(seq 50); do [ -s %s ] && exit 0; sleep 0.1; done; exit 1", out)); err != nil {
		t.Fatal("command never ran")
	}
	data, _ := os.ReadFile(out)
	if string(data) != "cmds\n" {
		t.Errorf("output %q", data)
	}
}

func resolve(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func TestUpHandlesDottedNamesAndFocus(t *testing.T) {
	c := newServer(t)
	ctx := layout.Context{Project: "dots", Root: t.TempDir()}
	windows := expand(t, `windows: [{name: first}, {name: api.v2, focus: true}, {name: "a:b"}]`, ctx)
	e := &Engine{Tmux: c, Width: 80, Height: 24}
	if _, err := e.Up(Session{Name: "dots", Windows: windows}); err != nil {
		t.Fatal(err)
	}
	active, err := c.Run("display-message", "-p", "-t", "=dots:", "#{window_name}")
	if err != nil {
		t.Fatal(err)
	}
	if active != "api.v2" {
		t.Errorf("active window %q", active)
	}
	names, _ := c.WindowNames("dots")
	if !slices.Equal(names, []string{"first", "api.v2", "a:b"}) {
		t.Errorf("windows %v", names)
	}
}

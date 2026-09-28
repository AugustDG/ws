package layout

import (
	"math"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func parse(t *testing.T, src string) []Window {
	t.Helper()
	var f File
	if err := yaml.Unmarshal([]byte(src), &f); err != nil {
		t.Fatal(err)
	}
	return f.Windows
}

var ctx = Context{
	Project: "viber",
	Root:    "/r/viber",
	HomeDir: "/home",
	Worktrees: []Worktree{
		{Name: "main", Path: "/r/viber"},
		{Name: "1", Path: "/pool/1/viber"},
		{Name: "2", Path: "/pool/2/viber"},
	},
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   []Size
		want []float64
	}{
		{[]Size{{}, {}}, []float64{50, 50}},
		{[]Size{Percent(70), {}}, []float64{70, 30}},
		{[]Size{Percent(20), {}, {}}, []float64{20, 40, 40}},
		{[]Size{Percent(1), Percent(3)}, []float64{25, 75}},
	}
	for _, tt := range tests {
		got, err := Normalize(tt.in)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("Normalize(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
	if _, err := Normalize([]Size{Percent(60), Percent(40), {}}); err == nil {
		t.Error("expected an error when set sizes leave nothing")
	}
}

func TestSplitPercents(t *testing.T) {
	// Three equal columns: split 67% off the whole, then 50% of that.
	if got := SplitPercents([]float64{100.0 / 3, 100.0 / 3, 100.0 / 3}); !slices.Equal(got, []int{67, 50}) {
		t.Errorf("got %v", got)
	}
	if got := SplitPercents([]float64{70, 30}); !slices.Equal(got, []int{30}) {
		t.Errorf("got %v", got)
	}
}

func TestExpandWorktreeGrid(t *testing.T) {
	windows := parse(t, `
windows:
  - name: code
    columns:
      - for_each: worktree
        rows:
          - size: 70%
            cmd: nvim
          - dir: web
            cmd: echo {name} {index}
  - name: misc
    dir: "~"
`)
	got, err := Expand(windows, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d windows", len(got))
	}

	code := got[0].Root
	if code.Split != Columns || len(code.Children) != 3 {
		t.Fatalf("code: split %v with %d children", code.Split, len(code.Children))
	}
	for i, col := range code.Children {
		if math.Abs(col.Pct-100.0/3) > 1e-9 {
			t.Errorf("column %d share %v", i, col.Pct)
		}
		if col.Split != Rows || col.Children[0].Pct != 70 || col.Children[1].Pct != 30 {
			t.Errorf("column %d rows wrong: %+v", i, col.Children)
		}
		wt := ctx.Worktrees[i]
		if col.Children[0].Dir != wt.Path {
			t.Errorf("column %d top dir %s, want %s", i, col.Children[0].Dir, wt.Path)
		}
		if col.Children[1].Dir != wt.Path+"/web" {
			t.Errorf("column %d bottom dir %s", i, col.Children[1].Dir)
		}
		wantCmd := "echo " + wt.Name + " " + string(rune('0'+i))
		if col.Children[1].Cmds[0] != wantCmd {
			t.Errorf("column %d cmd %q, want %q", i, col.Children[1].Cmds[0], wantCmd)
		}
	}
	if got[1].Root.Dir != "/home" {
		t.Errorf("misc dir %s", got[1].Root.Dir)
	}
}

func TestExpandForEachWindow(t *testing.T) {
	windows := parse(t, `
windows:
  - name: agent
    for_each: worktree
    cmd: claude
`)
	got, err := Expand(windows, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, w := range got {
		names = append(names, w.Name+"@"+w.Root.Dir)
	}
	want := []string{"agent-0@/r/viber", "agent-1@/pool/1/viber", "agent-2@/pool/2/viber"}
	if !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
}

func TestExpandWithoutWorktreesUsesRoot(t *testing.T) {
	windows := parse(t, `
windows:
  - columns: [{for_each: worktree}]
`)
	got, err := Expand(windows, Context{Project: "p", Root: "/p"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "win-1" || len(got[0].Root.Children) != 1 || got[0].Root.Children[0].Dir != "/p" {
		t.Errorf("got %+v", got[0])
	}
}

func TestExpandPreset(t *testing.T) {
	windows := parse(t, `
windows:
  - name: logs
    layout: even-vertical
    dir: logs
    panes:
      - cmd: tail -f a.log
      - dir: /var/log
`)
	got, err := Expand(windows, ctx)
	if err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.Preset != "even-vertical" || r.Root.Split != Preset {
		t.Fatalf("got %+v", r)
	}
	if r.Root.Children[0].Dir != "/r/viber/logs" || r.Root.Children[1].Dir != "/var/log" {
		t.Errorf("dirs %s %s", r.Root.Children[0].Dir, r.Root.Children[1].Dir)
	}
}

func TestExpandErrors(t *testing.T) {
	tests := map[string]string{
		"columns and rows": `windows: [{columns: [{}], rows: [{}]}]`,
		"cmd on split":     `windows: [{cmd: x, columns: [{}, {}]}]`,
		"bad for_each":     `windows: [{columns: [{for_each: branch}]}]`,
		"size on window":   `windows: [{size: 50%}]`,
		"duplicate names":  `windows: [{name: a}, {name: a}]`,
		"panes and splits": `windows: [{panes: [{}], columns: [{}]}]`,
		"oversized":        `windows: [{columns: [{size: 80%}, {size: 30%}, {}]}]`,
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Expand(parse(t, src), ctx); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestSizeYAML(t *testing.T) {
	var n Node
	if err := yaml.Unmarshal([]byte("size: 70%"), &n); err != nil || n.Size != Percent(70) {
		t.Errorf("got %v, %v", n.Size, err)
	}
	if err := yaml.Unmarshal([]byte("size: 150%"), &n); err == nil {
		t.Error("expected an error for 150%")
	}
	out, _ := yaml.Marshal(Node{Size: Percent(30), Cmd: Strings{"a"}})
	if got := strings.TrimSpace(string(out)); got != "size: 30%\ncmd: a" {
		t.Errorf("marshal = %q", got)
	}
}

func TestExpandForEachWindowWithProjectPlaceholder(t *testing.T) {
	got, err := Expand(parse(t, `windows: [{name: "{project}-dev", for_each: worktree}]`), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "viber-dev-0" || got[2].Name != "viber-dev-2" {
		t.Errorf("names %s, %s", got[0].Name, got[2].Name)
	}
}

package capture

import (
	"strings"
	"testing"

	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/tmux"
)

const home = "/home/me"

// fakeRepo treats /home/me/viber as a main checkout and /home/me/.treehouse/
// pool/N/viber as its linked worktrees.
func fakeRepo(dir string) (RepoInfo, bool) {
	switch {
	case strings.HasPrefix(dir, home+"/viber"):
		return RepoInfo{Top: home + "/viber", CommonDir: home + "/viber/.git"}, true
	case strings.HasPrefix(dir, home+"/.treehouse/pool/"):
		parts := strings.Split(strings.TrimPrefix(dir, home+"/.treehouse/pool/"), "/")
		top := home + "/.treehouse/pool/" + parts[0] + "/viber"
		return RepoInfo{Top: top, CommonDir: home + "/viber/.git", Linked: true}, true
	}
	return RepoInfo{}, false
}

func gridWindows(t *testing.T) []Window {
	t.Helper()
	cell, err := tmux.ParseLayout("3739,340x77,0,0{117x77,0,0[117x53,0,0,0,117x23,0,54,3],113x77,118,0[113x53,118,0,1,113x23,118,54,4],108x77,232,0[108x53,232,0,2,108x23,232,54,5]}")
	if err != nil {
		t.Fatal(err)
	}
	wt := func(n string) string { return home + "/.treehouse/pool/" + n + "/viber" }
	misc, _ := tmux.ParseLayout("e2f0,340x77,0,0{201x77,0,0,6,138x77,202,0,7}")
	return []Window{
		{Name: "viber", Cell: cell, Active: true, Panes: []Pane{
			{Dir: home + "/viber"}, {Dir: home + "/viber"},
			{Dir: wt("1")}, {Dir: wt("1")},
			{Dir: wt("2")}, {Dir: wt("2")},
		}},
		{Name: "misc", Cell: misc, Panes: []Pane{{Dir: home}, {Dir: home}}},
	}
}

func encode(t *testing.T, v any) string {
	t.Helper()
	data, err := project.EncodeYAML(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTemplatizeGeneralizesWorktreeGrid(t *testing.T) {
	res := Templatize(gridWindows(t), Options{Root: home, Home: home, Generalize: true, Repo: fakeRepo})

	if res.Root != home+"/viber" {
		t.Errorf("root = %s", res.Root)
	}
	if res.Worktrees == nil || *res.Worktrees != (project.Worktrees{Source: "treehouse", Count: 2}) {
		t.Errorf("worktrees = %+v", res.Worktrees)
	}
	want := `windows:
  - name: viber
    columns:
      - for_each: worktree
        rows:
          - size: 70%
          - {}
  - name: misc
    dir: "~"
    columns:
      - size: 59%
      - {}
`
	if got := encode(t, layout.File{Windows: res.Windows}); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTemplatizeWithoutGeneralize(t *testing.T) {
	res := Templatize(gridWindows(t), Options{Root: home, Home: home, Repo: fakeRepo})
	if res.Worktrees != nil {
		t.Error("should not generalize")
	}
	cols := res.Windows[0].Columns
	if len(cols) != 3 || cols[0].Dir != "viber" || cols[1].Dir != ".treehouse/pool/1/viber" {
		t.Errorf("columns = %+v", cols)
	}
}

func TestTemplatizeRejectsMismatchedColumns(t *testing.T) {
	ws := gridWindows(t)
	ws[0].Panes[3].Dir = home + "/.treehouse/pool/1/viber/web" // one column differs
	res := Templatize(ws, Options{Root: home, Home: home, Generalize: true, Repo: fakeRepo})
	if res.Worktrees != nil {
		t.Error("columns differ; should not generalize")
	}
}

func TestExpandRoundTrip(t *testing.T) {
	res := Templatize(gridWindows(t), Options{Root: home, Home: home, Generalize: true, Repo: fakeRepo})
	got, err := layout.Expand(res.Windows, layout.Context{
		Project: "viber", Root: res.Root, HomeDir: home,
		Worktrees: []layout.Worktree{
			{Name: "main", Path: home + "/viber"},
			{Name: "1", Path: home + "/.treehouse/pool/1/viber"},
			{Name: "2", Path: home + "/.treehouse/pool/2/viber"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	leaves := got[0].Root.Leaves()
	orig := gridWindows(t)[0].Panes
	if len(leaves) != len(orig) {
		t.Fatalf("got %d panes, want %d", len(leaves), len(orig))
	}
	for i := range leaves {
		if leaves[i].Dir != orig[i].Dir {
			t.Errorf("pane %d dir %s, want %s", i, leaves[i].Dir, orig[i].Dir)
		}
	}
	if got[1].Root.Dir != home {
		t.Errorf("misc dir = %s", got[1].Root.Dir)
	}
}

func TestRoundSizesStayLoadable(t *testing.T) {
	for _, widths := range [][]float64{{170, 168, 1}, {170, 1, 168}, {98, 1, 1}} {
		var total float64
		for _, w := range widths {
			total += w
		}
		kids := make([]layout.Node, len(widths))
		for i, w := range widths {
			kids[i].Size = layout.Percent(w * 100 / total)
		}
		roundSizes(kids)
		var sizes []layout.Size
		for _, k := range kids {
			if k.Size.Set && (k.Size.Pct <= 0 || k.Size.Pct >= 100) {
				t.Errorf("%v: size %v out of range", widths, k.Size.Pct)
			}
			sizes = append(sizes, k.Size)
		}
		if _, err := layout.Normalize(sizes); err != nil {
			t.Errorf("%v: %v", widths, err)
		}
	}
}

func TestTemplatizeKeepsIncompatibleRepeatConcrete(t *testing.T) {
	ws := gridWindows(t)
	// A second window repeating over only two of the worktrees.
	two, _ := tmux.ParseLayout("5e1a,200x50,0,0{100x50,0,0,1,99x50,101,0,2}")
	ws = append(ws, Window{Name: "pair", Cell: two, Panes: []Pane{
		{Dir: home + "/viber"}, {Dir: home + "/.treehouse/pool/1/viber"},
	}})
	res := Templatize(ws, Options{Root: home, Home: home, Generalize: true, Repo: fakeRepo})
	if res.Windows[0].Columns[0].ForEach == "" {
		t.Error("first window should generalize")
	}
	if pair := res.Windows[2].Columns; len(pair) != 2 || pair[0].ForEach != "" {
		t.Errorf("pair should stay concrete: %+v", pair)
	}
}

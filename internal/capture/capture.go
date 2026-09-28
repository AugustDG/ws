// Package capture turns concrete windows (a running session, or an imported
// tmuxinator project) into layout YAML: percentage sizes, relative dirs, and
// `for_each: worktree` where columns or rows repeat across a repo's worktrees.
package capture

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/tmux"
)

// Window is one concrete window. Either Cell (a split tree) or Preset is
// set. Panes are in depth-first order, matching Cell.Leaves().
type Window struct {
	Name   string
	Cell   *tmux.Cell
	Preset string
	Panes  []Pane
	Active bool
}

// Pane is one concrete pane.
type Pane struct {
	Dir    string // absolute
	Cmds   []string
	Active bool
}

// Options control how windows are templatized.
type Options struct {
	Root       string // dirs under it become relative
	Home       string
	Generalize bool     // detect worktree repeats
	Repo       RepoFunc // looks up a dir's repo; defaults to git
}

// Result is the templatized form.
type Result struct {
	Root      string // may differ from Options.Root when generalized
	Windows   []layout.Window
	Worktrees *project.Worktrees // set when a window was generalized
	Notes     []string
}

// Templatize converts windows into layout form.
func Templatize(windows []Window, opts Options) Result {
	if opts.Repo == nil {
		opts.Repo = GitRepo
	}
	res := Result{Root: opts.Root}

	var out []layout.Window
	for i, w := range windows {
		lw := toLayout(w)
		lw.Focus = w.Active && i > 0
		if opts.Generalize {
			if g, ok := generalize(lw.Node, opts); ok {
				switch {
				case res.Worktrees == nil:
					res.Root, res.Worktrees = g.main, &g.worktrees
					fallthrough
				case g.compatible(res.Root, *res.Worktrees):
					lw.Node = g.node
					res.Notes = append(res.Notes, g.note(w.Name, opts.Home))
					if note, ok := g.droppedNote(w.Name); ok {
						res.Notes = append(res.Notes, note)
					}
				default:
					res.Notes = append(res.Notes, fmt.Sprintf(
						"window %s repeats over different worktrees than an earlier window; kept as captured", w.Name))
				}
			}
		}
		out = append(out, lw)
	}

	for i := range out {
		relativize(&out[i], res.Root, opts.Home)
	}
	res.Windows = out
	return res
}

func toLayout(w Window) layout.Window {
	if w.Cell == nil {
		lw := layout.Window{Name: w.Name, Layout: w.Preset}
		for _, p := range w.Panes {
			lw.Panes = append(lw.Panes, leaf(p, false))
		}
		return lw
	}
	leaves := w.Panes
	var build func(c *tmux.Cell, first bool) layout.Node
	build = func(c *tmux.Cell, first bool) layout.Node {
		if c.Split == tmux.CellLeaf {
			p := leaves[0]
			leaves = leaves[1:]
			return leaf(p, first)
		}
		var n layout.Node
		var kids []layout.Node
		total := 0
		for _, ch := range c.Children {
			total += ch.Extent(c.Split)
		}
		for i, ch := range c.Children {
			kid := build(ch, first && i == 0)
			kid.Size = layout.Percent(float64(ch.Extent(c.Split)) * 100 / float64(total))
			kids = append(kids, kid)
		}
		roundSizes(kids)
		if c.Split == tmux.CellColumns {
			n.Columns = kids
		} else {
			n.Rows = kids
		}
		return n
	}
	return layout.Window{Name: w.Name, Node: build(w.Cell, true)}
}

func leaf(p Pane, first bool) layout.Node {
	return layout.Node{Dir: p.Dir, Cmd: p.Cmds, Focus: p.Active && !first}
}

// roundSizes rounds to whole percents and drops the last size, which takes
// the remainder. Every pane keeps at least 1%. If the split is even anyway
// all sizes are dropped; hand-resized panes are rarely exact, so "even"
// allows sizeTolerance of drift.
func roundSizes(kids []layout.Node) {
	sizes := make([]float64, len(kids))
	for i := range kids {
		sizes[i] = math.Max(math.Round(kids[i].Size.Pct), 1)
	}
	lo, hi := slices.Min(sizes), slices.Max(sizes)
	for i := range kids {
		kids[i].Size = layout.Size{}
	}
	if hi-lo <= sizeTolerance {
		return
	}

	// Leave the last pane at least 1% by trimming the largest set sizes.
	set := sizes[:len(sizes)-1]
	for sum(set) > 99 {
		set[slices.Index(set, slices.Max(set))]--
	}
	for i, p := range set {
		kids[i].Size = layout.Percent(p)
	}
}

func sum(xs []float64) float64 {
	var t float64
	for _, x := range xs {
		t += x
	}
	return t
}

// relativize rewrites absolute dirs: the root becomes "", paths under it
// become relative and the rest are shortened with ~. Then identical child
// dirs are hoisted onto their parent.
func relativize(w *layout.Window, root, home string) {
	rel := func(d string) string {
		if !filepath.IsAbs(d) {
			return d // already relative, e.g. inside a for_each template
		}
		if root != "" && d == root {
			return ""
		}
		if r, ok := strings.CutPrefix(d, strings.TrimSuffix(root, "/")+"/"); ok && root != "" {
			return r
		}
		if d == home {
			return "~"
		}
		if r, ok := strings.CutPrefix(d, home+"/"); ok {
			return "~/" + r
		}
		return d
	}
	for i := range w.Panes {
		w.Panes[i].Dir = rel(w.Panes[i].Dir)
	}
	walk(&w.Node, func(n *layout.Node) { n.Dir = rel(n.Dir) })
	hoist(&w.Node)
}

func walk(n *layout.Node, fn func(*layout.Node)) {
	fn(n)
	for i := range n.Columns {
		walk(&n.Columns[i], fn)
	}
	for i := range n.Rows {
		walk(&n.Rows[i], fn)
	}
}

// hoist moves a dir shared by every child onto the parent. It doesn't hoist
// out of a for_each node, whose dirs are relative to each worktree.
func hoist(n *layout.Node) {
	kids := n.Children()
	if len(kids) == 0 {
		return
	}
	for i := range kids {
		hoist(&kids[i])
	}
	if n.Dir != "" {
		return
	}
	shared := kids[0].Dir
	for _, k := range kids {
		if k.Dir != shared || k.ForEach != "" {
			return
		}
	}
	n.Dir = shared
	for i := range kids {
		kids[i].Dir = ""
	}
}

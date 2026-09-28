package capture

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/project"
)

// sizeTolerance is how far apart (in percent) captured sizes can be and
// still count as equal.
const sizeTolerance = 3

type generalized struct {
	node      layout.Node
	main      string // the repo's main checkout, which becomes the root
	worktrees project.Worktrees
	dropped   []string // commands left out because worktrees ran different ones
}

func (g generalized) note(window, home string) string {
	return fmt.Sprintf("window %s repeats across %d worktrees of %s: written as for_each: worktree (%s, count %d)",
		window, g.worktrees.Count+1, abbrev(g.main, home), g.worktrees.Source, g.worktrees.Count)
}

// droppedNote lists commands that couldn't go in the template, if any.
func (g generalized) droppedNote(window string) (string, bool) {
	if len(g.dropped) == 0 {
		return "", false
	}
	return fmt.Sprintf("window %s: worktrees ran different commands, left out of the template: %s",
		window, strings.Join(g.dropped, ", ")), true
}

// compatible reports whether two windows repeat over the same worktrees,
// which they must to share one project's root and worktree config.
func (g generalized) compatible(main string, wts project.Worktrees) bool {
	return g.main == main && g.worktrees == wts
}

// generalize checks whether a window's top-level columns or rows are the
// same layout repeated once per worktree of one repo. If so it returns the
// split with a single for_each: worktree child in their place.
func generalize(n layout.Node, opts Options) (generalized, bool) {
	kids := n.Children()
	if len(kids) < 2 {
		return generalized{}, false
	}

	var tops []string
	var common string
	main := ""
	templates := make([]layout.Node, len(kids))
	for i, k := range kids {
		if k.Size.Set {
			return generalized{}, false // uneven split; not a plain repeat
		}
		info, ok := repoOf(k, opts.Repo)
		if !ok || slices.Contains(tops, info.Top) || (common != "" && info.CommonDir != common) {
			return generalized{}, false
		}
		tops, common = append(tops, info.Top), info.CommonDir
		if !info.Linked && main == "" {
			main = info.Top
		}
		templates[i] = relativeTo(k, info.Top)
	}
	for _, t := range templates[1:] {
		if !sameShape(templates[0], t) {
			return generalized{}, false
		}
	}
	if main == "" {
		main = tops[0]
	}

	var labels []string
	for _, t := range tops {
		labels = append(labels, abbrev(t, opts.Home))
	}
	tmpl, dropped := sharedCommands(templates, labels)
	tmpl.ForEach = layout.ForEachWorktree
	out := n
	if len(n.Rows) > 0 {
		out.Rows = []layout.Node{tmpl}
	} else {
		out.Columns = []layout.Node{tmpl}
	}

	source := "git"
	for _, t := range tops {
		if strings.Contains(t, "/.treehouse/") {
			source = "treehouse"
		}
	}
	return generalized{
		node: out, main: main, dropped: dropped,
		worktrees: project.Worktrees{Source: source, Count: len(kids) - 1},
	}, true
}

// repoOf returns the single repo every pane under n lives in.
func repoOf(n layout.Node, repo RepoFunc) (RepoInfo, bool) {
	var info RepoInfo
	ok := true
	first := true
	walk(&n, func(c *layout.Node) {
		if len(c.Children()) > 0 || !ok {
			return
		}
		ri, found := repo(c.Dir)
		if !found || (!first && ri.Top != info.Top) {
			ok = false
			return
		}
		info, first = ri, false
	})
	return info, ok && !first
}

// relativeTo copies n with every dir made relative to top.
func relativeTo(n layout.Node, top string) layout.Node {
	n = clone(n)
	walk(&n, func(c *layout.Node) {
		if c.Dir == "" {
			return
		}
		r, err := filepath.Rel(top, c.Dir)
		switch {
		case err != nil || strings.HasPrefix(r, ".."):
		case r == ".":
			c.Dir = ""
		default:
			c.Dir = r
		}
	})
	return n
}

func clone(n layout.Node) layout.Node {
	n.Columns = cloneAll(n.Columns)
	n.Rows = cloneAll(n.Rows)
	n.Cmd = slices.Clone(n.Cmd)
	return n
}

func cloneAll(nodes []layout.Node) []layout.Node {
	if nodes == nil {
		return nil
	}
	out := make([]layout.Node, len(nodes))
	for i, c := range nodes {
		out[i] = clone(c)
	}
	return out
}

// sharedCommands merges same-shaped templates into one. A pane keeps its
// command when every copy runs the same one; otherwise the command is left
// out and reported as "cmd in <label>" for each copy that ran something.
func sharedCommands(templates []layout.Node, labels []string) (layout.Node, []string) {
	merged := clone(templates[0])
	mergedLeaves := leaves(&merged)
	copies := make([][]*layout.Node, len(templates))
	for i := range templates {
		copies[i] = leaves(&templates[i])
	}

	var dropped []string
	for j, leaf := range mergedLeaves {
		same := true
		for i := range copies {
			same = same && slices.Equal(copies[i][j].Cmd, leaf.Cmd)
		}
		if same {
			continue
		}
		leaf.Cmd = nil
		for i := range copies {
			if cmd := strings.Join(copies[i][j].Cmd, "; "); cmd != "" {
				dropped = append(dropped, fmt.Sprintf("%s in %s", cmd, labels[i]))
			}
		}
	}
	return merged, dropped
}

func leaves(n *layout.Node) []*layout.Node {
	var out []*layout.Node
	walk(n, func(c *layout.Node) {
		if len(c.Children()) == 0 {
			out = append(out, c)
		}
	})
	return out
}

// sameShape compares two templates' structure, dirs and sizes, allowing
// small size differences. Commands are merged separately by sharedCommands.
func sameShape(a, b layout.Node) bool {
	if a.Dir != b.Dir || a.Size.Set != b.Size.Set ||
		math.Abs(a.Size.Pct-b.Size.Pct) > sizeTolerance ||
		len(a.Columns) != len(b.Columns) || len(a.Rows) != len(b.Rows) {
		return false
	}
	ak, bk := a.Children(), b.Children()
	for i := range ak {
		if !sameShape(ak[i], bk[i]) {
			return false
		}
	}
	return true
}

func abbrev(p, home string) string {
	if r, ok := strings.CutPrefix(p, home+"/"); ok {
		return "~/" + r
	}
	return p
}

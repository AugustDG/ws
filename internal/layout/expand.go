package layout

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Context carries the values a layout is expanded against.
type Context struct {
	Project   string
	Root      string
	HomeDir   string
	Worktrees []Worktree // index 0 is the project root itself
}

// Worktree is one checkout that for_each expands over.
type Worktree struct {
	Name string
	Path string
}

// Split is how a resolved pane divides its area.
type Split int

const (
	Leaf Split = iota
	Columns
	Rows
	Preset
)

// Pane is a resolved node: absolute dir, expanded commands and a share of
// its parent that has been normalized.
type Pane struct {
	Dir      string
	Cmds     []string
	Focus    bool
	Split    Split
	Pct      float64
	Children []*Pane
}

// Resolved is a window ready to be built.
type Resolved struct {
	Name   string
	Preset string // tmux preset for Preset roots
	Focus  bool
	Root   *Pane
}

// FirstLeaf returns the top-left leaf. A subtree's pane is created in this
// leaf's dir before it's split further.
func (p *Pane) FirstLeaf() *Pane {
	for p.Split != Leaf && len(p.Children) > 0 {
		p = p.Children[0]
	}
	return p
}

// Leaves returns leaves in depth-first order, the order tmux numbers panes.
func (p *Pane) Leaves() []*Pane {
	if p.Split == Leaf {
		return []*Pane{p}
	}
	var out []*Pane
	for _, c := range p.Children {
		out = append(out, c.Leaves()...)
	}
	return out
}

// scope holds the values that {placeholders} in names, dirs and commands
// expand to. Inside a for_each, path/name/index describe that worktree.
type scope struct {
	ctx   Context
	wt    Worktree
	index int
}

func (s scope) in(i int) scope {
	return scope{ctx: s.ctx, wt: s.ctx.Worktrees[i], index: i}
}

func (s scope) expand(str string) string {
	return strings.NewReplacer(
		"{project}", s.ctx.Project,
		"{root}", s.ctx.Root,
		"{path}", s.wt.Path,
		"{name}", s.wt.Name,
		"{index}", strconv.Itoa(s.index),
	).Replace(str)
}

// dir resolves a node's dir: empty inherits parent, ~ is home, relative is
// relative to parent.
func (s scope) dir(raw, parent string) string {
	d := s.expand(raw)
	switch {
	case d == "":
		return parent
	case d == "~":
		return s.ctx.HomeDir
	case strings.HasPrefix(d, "~/"):
		return filepath.Join(s.ctx.HomeDir, d[2:])
	case filepath.IsAbs(d):
		return filepath.Clean(d)
	}
	return filepath.Join(parent, d)
}

// Expand resolves windows against ctx, repeating for_each windows and nodes
// once per worktree.
func Expand(windows []Window, ctx Context) ([]Resolved, error) {
	if len(ctx.Worktrees) == 0 {
		ctx.Worktrees = []Worktree{{Name: "main", Path: ctx.Root}}
	}
	root := scope{ctx: ctx}.in(0)

	var out []Resolved
	for i, w := range windows {
		name := w.Name
		if name == "" {
			name = fmt.Sprintf("win-%d", i+1)
		}
		if err := w.Validate(); err != nil {
			return nil, fmt.Errorf("window %s: %w", name, err)
		}

		scopes := []scope{root}
		if w.ForEach == ForEachWorktree {
			scopes = nil
			for j := range ctx.Worktrees {
				scopes = append(scopes, root.in(j))
			}
			if !perWorktree(name) {
				name += "-{index}"
			}
		}
		for _, s := range scopes {
			r, err := expandWindow(w, s.expand(name), s)
			if err != nil {
				return nil, fmt.Errorf("window %s: %w", name, err)
			}
			out = append(out, r)
		}
	}

	seen := map[string]bool{}
	for _, r := range out {
		if seen[r.Name] {
			return nil, fmt.Errorf("duplicate window name %q", r.Name)
		}
		seen[r.Name] = true
	}
	return out, nil
}

// perWorktree reports whether a name already differs between worktrees.
func perWorktree(name string) bool {
	for _, v := range []string{"{index}", "{name}", "{path}"} {
		if strings.Contains(name, v) {
			return true
		}
	}
	return false
}

func expandWindow(w Window, name string, s scope) (Resolved, error) {
	dir := s.dir(w.Dir, s.wt.Path)
	r := Resolved{Name: name, Focus: w.Focus}

	if len(w.Panes) > 0 {
		r.Preset = w.Layout
		if r.Preset == "" {
			r.Preset = "tiled"
		}
		r.Root = &Pane{Dir: dir, Split: Preset}
		for _, p := range w.Panes {
			leaf, err := expandNode(p, s, dir)
			if err != nil {
				return r, err
			}
			r.Root.Children = append(r.Root.Children, leaf)
		}
		return r, nil
	}

	node := w.Node
	node.ForEach, node.Dir = "", ""
	root, err := expandNode(node, s, dir)
	r.Root = root
	return r, err
}

func expandNode(n Node, s scope, parentDir string) (*Pane, error) {
	p := &Pane{Dir: s.dir(n.Dir, parentDir), Focus: n.Focus, Split: Leaf}
	children := n.Children()
	if len(children) == 0 {
		for _, c := range n.Cmd {
			p.Cmds = append(p.Cmds, s.expand(c))
		}
		return p, nil
	}

	p.Split = Columns
	if len(n.Rows) > 0 {
		p.Split = Rows
	}
	var sizes []Size
	for _, c := range children {
		copies := []scope{s}
		if c.ForEach == ForEachWorktree {
			copies = nil
			for j := range s.ctx.Worktrees {
				copies = append(copies, s.in(j))
			}
		}
		for _, cs := range copies {
			base := p.Dir
			if c.ForEach == ForEachWorktree {
				base = cs.wt.Path
			}
			child, err := expandNode(c, cs, base)
			if err != nil {
				return nil, err
			}
			p.Children = append(p.Children, child)
			sizes = append(sizes, c.Size)
		}
	}

	shares, err := Normalize(sizes)
	if err != nil {
		return nil, err
	}
	for i, c := range p.Children {
		c.Pct = shares[i]
	}
	return p, nil
}

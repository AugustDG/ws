// Package layout defines the YAML layout model and expands it into a
// concrete pane tree for one project.
//
// A window is either a split tree (columns/rows nested to any depth) or a
// flat list of panes arranged by one of tmux's preset layouts. Any window,
// or any node inside a split, can set `for_each: worktree` to repeat once
// per worktree of the project.
package layout

import (
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ForEachWorktree repeats a node or window once per worktree.
const ForEachWorktree = "worktree"

// File is the on-disk format of a layout template.
type File struct {
	Windows []Window `yaml:"windows"`
}

// Window is one tmux window. Its inline Node is the root of the split tree;
// Layout+Panes is the alternative preset form.
type Window struct {
	Name   string `yaml:"name,omitempty"`
	Layout string `yaml:"layout,omitempty"`
	Panes  []Node `yaml:"panes,omitempty"`
	Node   `yaml:",inline"`
}

// Node is a pane (no columns or rows) or a split into columns or rows.
type Node struct {
	Size    Size    `yaml:"size,omitempty"`
	Dir     string  `yaml:"dir,omitempty"`
	Cmd     Strings `yaml:"cmd,omitempty"`
	Focus   bool    `yaml:"focus,omitempty"`
	ForEach string  `yaml:"for_each,omitempty"`
	Columns []Node  `yaml:"columns,omitempty"`
	Rows    []Node  `yaml:"rows,omitempty"`
}

// Size is a pane's share of its parent split, in percent. Unset sizes share
// whatever the set ones leave.
type Size struct {
	Pct float64
	Set bool
}

func Percent(p float64) Size { return Size{Pct: p, Set: true} }

func (s Size) IsZero() bool { return !s.Set }

func (s *Size) UnmarshalYAML(n *yaml.Node) error {
	raw := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(n.Value), "%"))
	p, err := strconv.ParseFloat(raw, 64)
	if err != nil || p <= 0 || p >= 100 {
		return fmt.Errorf("line %d: size %q must be a percentage between 0 and 100, like 70%%", n.Line, n.Value)
	}
	*s = Percent(p)
	return nil
}

func (s Size) MarshalYAML() (any, error) {
	return strconv.FormatFloat(s.Pct, 'f', -1, 64) + "%", nil
}

// Strings accepts either a single string or a list of strings.
type Strings []string

func (s *Strings) UnmarshalYAML(n *yaml.Node) error {
	switch {
	case n.Kind == yaml.ScalarNode && n.Tag == "!!null":
		*s = nil
	case n.Kind == yaml.ScalarNode:
		*s = Strings{n.Value}
	case n.Kind == yaml.SequenceNode:
		var list []string
		if err := n.Decode(&list); err != nil {
			return err
		}
		*s = list
	default:
		return fmt.Errorf("line %d: expected a string or a list of strings", n.Line)
	}
	return nil
}

func (s Strings) MarshalYAML() (any, error) {
	if len(s) == 1 {
		return s[0], nil
	}
	return []string(s), nil
}

// Validate checks the rules the YAML types can't express.
func (w Window) Validate() error {
	if err := checkForEach(w.ForEach); err != nil {
		return err
	}
	if w.Size.Set {
		return fmt.Errorf("size is only valid on panes inside columns or rows")
	}
	if len(w.Panes) > 0 && (len(w.Columns) > 0 || len(w.Rows) > 0) {
		return fmt.Errorf("use either panes (with a preset layout) or columns/rows, not both")
	}
	if w.Layout != "" && len(w.Panes) == 0 {
		return fmt.Errorf("layout %q needs a panes list", w.Layout)
	}
	for _, p := range w.Panes {
		if len(p.Columns) > 0 || len(p.Rows) > 0 || p.ForEach != "" || p.Size.Set {
			return fmt.Errorf("panes in a preset layout can't nest, repeat or set a size")
		}
	}
	return w.Node.validate()
}

func (n Node) validate() error {
	if err := checkForEach(n.ForEach); err != nil {
		return err
	}
	if len(n.Columns) > 0 && len(n.Rows) > 0 {
		return fmt.Errorf("a node can have columns or rows, not both")
	}
	if len(n.Cmd) > 0 && (len(n.Columns) > 0 || len(n.Rows) > 0) {
		return fmt.Errorf("cmd is only valid on panes without columns or rows")
	}
	for _, c := range n.Children() {
		if err := c.validate(); err != nil {
			return err
		}
	}
	return nil
}

// Children returns the node's columns or rows, whichever is set.
func (n Node) Children() []Node {
	if len(n.Rows) > 0 {
		return n.Rows
	}
	return n.Columns
}

func checkForEach(v string) error {
	if v != "" && v != ForEachWorktree {
		return fmt.Errorf("unknown for_each %q (only %q is supported)", v, ForEachWorktree)
	}
	return nil
}

// Package tmuxinator reads tmuxinator project files into capture windows, so
// they go through the same templatizing as a captured session.
package tmuxinator

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/AugustDG/ws/internal/capture"
	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/tmux"
)

// Project is what an import yields.
type Project struct {
	Name     string
	Root     string
	OnStart  string
	OnStop   string
	Windows  []capture.Window
	Warnings []string
}

type file struct {
	Name           string                 `yaml:"name"`
	Root           string                 `yaml:"root"`
	ProjectRoot    string                 `yaml:"project_root"`
	PreWindow      layout.Strings         `yaml:"pre_window"`
	OnProjectStart layout.Strings         `yaml:"on_project_start"`
	OnProjectStop  layout.Strings         `yaml:"on_project_stop"`
	Windows        []map[string]yaml.Node `yaml:"windows"`
}

type window struct {
	Root   string      `yaml:"root"`
	Layout string      `yaml:"layout"`
	Panes  []yaml.Node `yaml:"panes"`
}

var (
	rawLayout = regexp.MustCompile(`^[0-9a-f]{4},\d+x\d+`)
	// cdCommand matches `cd <path>`, optionally followed by `&& <rest>`.
	// Paths using shell features ($VAR, -, globs) are left as commands.
	cdCommand = regexp.MustCompile(`^cd\s+(?:"([^"$]+)"|'([^']+)'|([^\s;&|$` + "`" + `'"*?]+))\s*(?:&&\s*(.+))?$`)
)

// Parse reads a tmuxinator file. home expands ~ in paths.
func Parse(data []byte, home string) (Project, error) {
	if strings.Contains(string(data), "<%") {
		return Project{}, fmt.Errorf("ERB templates aren't supported; replace the <%% %%> parts with plain values first")
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Project{}, err
	}
	root := f.Root
	if root == "" {
		root = f.ProjectRoot
	}
	p := Project{
		Name:    f.Name,
		Root:    expand(root, home, home),
		OnStart: strings.Join(f.OnProjectStart, "\n"),
		OnStop:  strings.Join(f.OnProjectStop, "\n"),
	}

	for _, entry := range f.Windows {
		for name, node := range entry {
			w, warn, err := parseWindow(name, node, p.Root, home, f.PreWindow)
			if err != nil {
				return p, fmt.Errorf("window %s: %w", name, err)
			}
			if warn != "" {
				p.Warnings = append(p.Warnings, fmt.Sprintf("window %s: %s", name, warn))
			}
			p.Windows = append(p.Windows, w)
		}
	}
	return p, nil
}

func parseWindow(name string, node yaml.Node, root, home string, pre []string) (capture.Window, string, error) {
	w := capture.Window{Name: name}
	var win window
	switch node.Kind {
	case yaml.ScalarNode:
		// "name: command" is a single pane running command.
		w.Cell = &tmux.Cell{PaneID: -1}
		w.Panes = []capture.Pane{pane(cmds(node.Value), root, home, pre)}
		return w, "", nil
	case yaml.MappingNode:
		if err := node.Decode(&win); err != nil {
			return w, "", err
		}
	default:
		return w, "", fmt.Errorf("unsupported window form")
	}

	dir := expand(win.Root, root, home)
	for _, pn := range win.Panes {
		c, err := paneCmds(pn)
		if err != nil {
			return w, "", err
		}
		w.Panes = append(w.Panes, pane(c, dir, home, pre))
	}
	if len(w.Panes) == 0 {
		w.Panes = []capture.Pane{pane(nil, dir, home, pre)}
	}
	if len(w.Panes) == 1 {
		w.Cell = &tmux.Cell{PaneID: -1}
		return w, "", nil
	}

	if rawLayout.MatchString(win.Layout) {
		cell, err := tmux.ParseLayout(win.Layout)
		if err == nil && len(cell.Leaves()) == len(w.Panes) {
			w.Cell = cell
			return w, "", nil
		}
		w.Preset = "tiled"
		return w, "layout string doesn't match the pane count; using tiled", nil
	}
	w.Preset = win.Layout
	if w.Preset == "" {
		w.Preset = "tiled"
	}
	return w, "", nil
}

// paneCmds reads the forms tmuxinator allows for a pane: a command, a
// list of commands, or a single-key map of a pane title to commands.
func paneCmds(n yaml.Node) ([]string, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return nil, nil
		}
		return cmds(n.Value), nil
	case yaml.SequenceNode:
		var list layout.Strings
		err := n.Decode(&list)
		return list, err
	case yaml.MappingNode:
		var m map[string]layout.Strings
		if err := n.Decode(&m); err != nil {
			return nil, err
		}
		for _, v := range m {
			return v, nil
		}
	}
	return nil, fmt.Errorf("unsupported pane form at line %d", n.Line)
}

func cmds(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return []string{s}
}

// pane turns a leading `cd <dir>` into the pane's dir, since ws starts
// panes in their dir directly. Anything after `&&` stays a command.
func pane(cmds []string, dir, home string, pre []string) capture.Pane {
	if len(cmds) > 0 {
		if m := cdCommand.FindStringSubmatch(strings.TrimSpace(cmds[0])); m != nil && m[1]+m[2]+m[3] != "-" {
			dir = expand(m[1]+m[2]+m[3], dir, home)
			cmds = cmds[1:]
			if m[4] != "" {
				cmds = append([]string{m[4]}, cmds...)
			}
		}
	}
	return capture.Pane{Dir: dir, Cmds: append(append([]string{}, pre...), cmds...)}
}

func expand(p, base, home string) string {
	switch {
	case p == "":
		return base
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/"):
		return filepath.Join(home, p[2:])
	case filepath.IsAbs(p):
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

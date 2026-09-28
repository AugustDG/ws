package capture

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AugustDG/ws/internal/tmux"
)

// FromSession reads a running session's windows and its start directory.
// Each pane records as much of its foreground program as commands asks for,
// preferring the line as typed when the shell-init hook recorded one.
func FromSession(c *tmux.Client, session string, commands Commands) ([]Window, string, error) {
	target := tmux.Exact(session)
	root, err := c.Run("display-message", "-p", "-t", target, "#{session_path}")
	if err != nil {
		return nil, "", err
	}

	panes, err := c.Lines("list-panes", "-s", "-t", target, "-F", "#{pane_id}\t#{pane_current_path}\t#{pane_active}\t#{pane_pid}\t#{"+tmux.TypedCommandOption+"}")
	if err != nil {
		return nil, "", err
	}
	byID := map[int]Pane{}
	for _, p := range panes {
		id, err := strconv.Atoi(strings.TrimPrefix(p[0], "%"))
		if err != nil || len(p) < 5 {
			continue
		}
		pane := Pane{Dir: p[1], Active: p[2] == "1"}
		typed := strings.Join(p[4:], "\t") // the typed line may itself hold tabs
		if pid, err := strconv.Atoi(p[3]); err == nil {
			if cmd := paneCommand(pid, typed, commands); cmd != "" {
				pane.Cmds = []string{cmd}
			}
		}
		byID[id] = pane
	}

	rows, err := c.Lines("list-windows", "-t", target, "-F", "#{window_name}\t#{window_layout}\t#{window_active}")
	if err != nil {
		return nil, "", err
	}
	var out []Window
	for _, r := range rows {
		cell, err := tmux.ParseLayout(r[1])
		if err != nil {
			return nil, "", fmt.Errorf("window %s: %w", r[0], err)
		}
		w := Window{Name: r[0], Cell: cell, Active: r[2] == "1"}
		for _, leaf := range cell.Leaves() {
			w.Panes = append(w.Panes, byID[leaf.PaneID])
		}
		out = append(out, w)
	}
	if root == "" {
		root = commonDir(out)
	}
	return out, root, nil
}

// commonDir is the deepest directory containing every pane, used as the
// root for sessions started without a directory.
func commonDir(windows []Window) string {
	var common []string
	first := true
	for _, w := range windows {
		for _, p := range w.Panes {
			parts := strings.Split(filepath.Clean(p.Dir), "/")
			if first {
				common, first = parts, false
				continue
			}
			n := 0
			for n < len(common) && n < len(parts) && common[n] == parts[n] {
				n++
			}
			common = common[:n]
		}
	}
	if len(common) <= 1 {
		return "/"
	}
	return strings.Join(common, "/")
}

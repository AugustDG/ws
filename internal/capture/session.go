package capture

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/AugustDG/ws/internal/tmux"
)

// FromSession reads a running session's windows and its start directory.
func FromSession(c *tmux.Client, session string) ([]Window, string, error) {
	target := tmux.Exact(session)
	root, err := c.Run("display-message", "-p", "-t", target, "#{session_path}")
	if err != nil {
		return nil, "", err
	}

	panes, err := c.Lines("list-panes", "-s", "-t", target, "-F", "#{pane_id}\t#{pane_current_path}\t#{pane_active}")
	if err != nil {
		return nil, "", err
	}
	byID := map[int]Pane{}
	for _, p := range panes {
		id, err := strconv.Atoi(strings.TrimPrefix(p[0], "%"))
		if err != nil || len(p) < 3 {
			continue
		}
		byID[id] = Pane{Dir: p[1], Active: p[2] == "1"}
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
	return out, root, nil
}

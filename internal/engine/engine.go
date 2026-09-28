// Package engine builds tmux sessions from resolved layouts.
package engine

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/tmux"
)

// Session is everything needed to create one tmux session.
type Session struct {
	Name    string
	Env     map[string]string
	Windows []layout.Resolved
}

// Engine creates sessions on a tmux server. Width and Height size a new
// session before it's attached, so percentage splits land where intended.
type Engine struct {
	Tmux          *tmux.Client
	Width, Height int
}

// Result says what Up did.
type Result struct {
	Created bool
	Added   []string // windows added to a session that already existed
}

// Up creates the session, or adds any missing windows to it if it already
// runs. Existing windows are never touched.
func (e *Engine) Up(s Session) (Result, error) {
	if len(s.Windows) == 0 {
		return Result{}, fmt.Errorf("session %s has no windows", s.Name)
	}
	if !e.Tmux.HasSession(s.Name) {
		return Result{Created: true}, e.create(s)
	}

	existing, err := e.Tmux.WindowNames(s.Name)
	if err != nil {
		return Result{}, err
	}
	have := map[string]bool{}
	for _, n := range existing {
		have[n] = true
	}
	var res Result
	for _, w := range s.Windows {
		if have[w.Name] {
			continue
		}
		if _, err := e.addWindow(s.Name, w); err != nil {
			return res, err
		}
		res.Added = append(res.Added, w.Name)
	}
	return res, nil
}

func (e *Engine) create(s Session) error {
	first := s.Windows[0]
	args := []string{"new-session", "-d", "-s", s.Name, "-n", first.Name,
		"-c", first.Root.FirstLeaf().Dir, "-P", "-F", "#{pane_id}"}
	if e.Width > 0 && e.Height > 0 {
		args = append(args, "-x", strconv.Itoa(e.Width), "-y", strconv.Itoa(e.Height))
	}
	for _, k := range sortedKeys(s.Env) {
		args = append(args, "-e", k+"="+s.Env[k])
	}
	pane, err := e.Tmux.Run(args...)
	if err != nil {
		return err
	}

	// Windows are targeted through a pane id from here on: names can hold
	// "." or ":", which tmux would read as pane or session separators.
	focus, focused := pane, first.Focus
	fail := func(err error) error {
		e.Tmux.KillSession(s.Name)
		return err
	}
	if err := e.fill(pane, first); err != nil {
		return fail(fmt.Errorf("window %s: %w", first.Name, err))
	}
	for _, w := range s.Windows[1:] {
		pane, err := e.addWindow(s.Name, w)
		if err != nil {
			return fail(err)
		}
		if w.Focus && !focused {
			focus, focused = pane, true
		}
	}
	if _, err := e.Tmux.Run("select-window", "-t", focus); err != nil {
		return fail(err)
	}
	return nil
}

// addWindow appends a window to the session and returns its first pane.
func (e *Engine) addWindow(session string, w layout.Resolved) (string, error) {
	pane, err := e.Tmux.Run("new-window", "-d", "-t", tmux.Exact(session)+":",
		"-n", w.Name, "-c", w.Root.FirstLeaf().Dir, "-P", "-F", "#{pane_id}")
	if err != nil {
		return "", err
	}
	if err := e.fill(pane, w); err != nil {
		return "", fmt.Errorf("window %s: %w", w.Name, err)
	}
	return pane, nil
}

// fill splits a window's first pane into the window's layout, then sends
// each leaf its commands.
func (e *Engine) fill(pane string, w layout.Resolved) error {
	panes := map[*layout.Pane]string{}
	var err error
	if w.Root.Split == layout.Preset {
		err = e.preset(pane, w, panes)
	} else {
		err = e.split(pane, w.Root, panes)
	}
	if err != nil {
		return err
	}

	for _, leaf := range w.Root.Leaves() {
		for _, cmd := range leaf.Cmds {
			if err := e.send(panes[leaf], cmd); err != nil {
				return err
			}
		}
	}
	for _, leaf := range w.Root.Leaves() {
		if leaf.Focus {
			_, err := e.Tmux.Run("select-pane", "-t", panes[leaf])
			return err
		}
	}
	_, err = e.Tmux.Run("select-pane", "-t", panes[w.Root.FirstLeaf()])
	return err
}

// split carves node's children out of pane one at a time, then recurses.
// pane already starts in node.FirstLeaf().Dir.
func (e *Engine) split(pane string, node *layout.Pane, panes map[*layout.Pane]string) error {
	if node.Split == layout.Leaf {
		panes[node] = pane
		return nil
	}
	flag := "-h"
	if node.Split == layout.Rows {
		flag = "-v"
	}

	shares := make([]float64, len(node.Children))
	for i, c := range node.Children {
		shares[i] = c.Pct
	}
	ids := []string{pane}
	for i, pct := range layout.SplitPercents(shares) {
		child := node.Children[i+1]
		id, err := e.Tmux.Run("split-window", flag, "-d", "-t", ids[i],
			"-l", strconv.Itoa(pct)+"%", "-c", child.FirstLeaf().Dir, "-P", "-F", "#{pane_id}")
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for i, c := range node.Children {
		if err := e.split(ids[i], c, panes); err != nil {
			return err
		}
	}
	return nil
}

// preset creates one pane per leaf, then lets tmux arrange them.
func (e *Engine) preset(pane string, w layout.Resolved, panes map[*layout.Pane]string) error {
	leaves := w.Root.Children
	panes[leaves[0]] = pane
	for _, leaf := range leaves[1:] {
		id, err := e.Tmux.Run("split-window", "-d", "-t", pane, "-c", leaf.Dir, "-P", "-F", "#{pane_id}")
		if err != nil {
			return err
		}
		panes[leaf] = id
		// Re-tile as we go so tmux never runs out of room for the next split.
		if _, err := e.Tmux.Run("select-layout", "-t", pane, "tiled"); err != nil {
			return err
		}
	}
	_, err := e.Tmux.Run("select-layout", "-t", pane, w.Preset)
	return err
}

func (e *Engine) send(pane, cmd string) error {
	if _, err := e.Tmux.Run("send-keys", "-t", pane, "-l", "--", cmd); err != nil {
		return err
	}
	_, err := e.Tmux.Run("send-keys", "-t", pane, "Enter")
	return err
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

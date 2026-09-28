// Package discover lists what the picker can open: running sessions and
// configured projects. Each origin is a Source; Collect merges them.
package discover

import (
	"sort"
	"time"

	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/tmux"
)

type Kind int

const (
	Session Kind = iota // a running session with no project file
	Project
)

func (k Kind) String() string {
	if k == Project {
		return "project"
	}
	return "session"
}

// Item is one picker entry.
type Item struct {
	Name     string
	Path     string
	Kind     Kind
	Running  bool
	External bool      // running, but not started by ws
	LastUsed time.Time // zero if never used
}

// State is running, external (running but not started by ws) or stopped.
func (it Item) State() string {
	switch {
	case it.External:
		return "external"
	case it.Running:
		return "running"
	}
	return "stopped"
}

// Source produces items. Errors are ignored by Collect so one broken
// origin doesn't empty the picker.
type Source func() ([]Item, error)

// Collect merges sources in order. A project that's running is shown once,
// as a running project. Items sort by when they were last used, the later
// of their own LastUsed and used[name], newest first. Never-used items
// follow, running ones first, otherwise in source order.
func Collect(used map[string]time.Time, sources ...Source) []Item {
	var items []Item
	byName := map[string]int{}
	for _, src := range sources {
		found, _ := src()
		for _, it := range found {
			if i, ok := byName[it.Name]; ok {
				items[i].Running = items[i].Running || it.Running
				items[i].External = items[i].External || it.External
				items[i].LastUsed = later(items[i].LastUsed, it.LastUsed)
				if it.Kind == Project {
					items[i].Kind, items[i].Path = Project, it.Path
				}
				continue
			}
			byName[it.Name] = len(items)
			items = append(items, it)
		}
	}
	for i := range items {
		items[i].LastUsed = later(items[i].LastUsed, used[items[i].Name])
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if !a.LastUsed.Equal(b.LastUsed) {
			return a.LastUsed.After(b.LastUsed)
		}
		return a.Running && !b.Running
	})
	return items
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// Sessions lists running tmux sessions.
func Sessions(c *tmux.Client) Source {
	return func() ([]Item, error) {
		sessions, err := c.Sessions()
		var out []Item
		for _, s := range sessions {
			out = append(out, Item{Name: s.Name, Path: s.Path, Kind: Session, Running: true, External: !s.Managed, LastUsed: s.LastUsed})
		}
		return out, err
	}
}

// Projects lists configured projects.
func Projects(cfg *project.Config) Source {
	return func() ([]Item, error) {
		projects, err := cfg.Projects()
		var out []Item
		for _, p := range projects {
			out = append(out, Item{Name: p.Name, Path: p.Root, Kind: Project})
		}
		return out, err
	}
}

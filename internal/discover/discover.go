// Package discover lists what the picker can open: running sessions,
// configured projects and remote workspaces. Each origin is a Source;
// Collect merges them.
package discover

import (
	"sort"
	"time"

	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/state"
	"github.com/AugustDG/ws/internal/tmux"
)

type Kind int

const (
	Session Kind = iota // a running session with no project file
	Project
	Remote // a workspace on another host, reached with ws ssh
)

func (k Kind) String() string {
	switch k {
	case Project:
		return "project"
	case Remote:
		return "remote"
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

	Host, Target string // for a Remote: what ws ssh is given

	// Via is set on items of the machine ws ssh connected from, as the
	// picker on a host sees them ("local").
	Via string `json:",omitempty"`
}

// Label is the kind as shown: "local session" for an item seen via the
// machine connected from. A remote stays "remote" wherever it's seen.
func (it Item) Label() string {
	if it.Via != "" && it.Kind != Remote {
		return it.Via + " " + it.Kind.String()
	}
	return it.Kind.String()
}

// Here reports whether the item belongs to this machine.
func (it Item) Here() bool { return it.Kind != Remote && it.Via == "" }

// State is running, external (running but not started by ws) or stopped.
// A remote item's state isn't known without connecting, so it's "remote".
func (it Item) State() string {
	switch {
	case it.Kind == Remote:
		return "remote"
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
// follow, running ones first, otherwise in source order. Only items of
// this machine merge by name, and used applies to them alone.
func Collect(used map[string]time.Time, sources ...Source) []Item {
	var items []Item
	byName := map[string]int{}
	for _, src := range sources {
		found, _ := src()
		for _, it := range found {
			key := it.Name
			if !it.Here() {
				key = it.Via + "\x00" + it.Kind.String() + "\x00" + it.Name
			}
			if i, ok := byName[key]; ok {
				items[i].Running = items[i].Running || it.Running
				items[i].External = items[i].External || it.External
				items[i].LastUsed = later(items[i].LastUsed, it.LastUsed)
				if it.Kind == Project {
					items[i].Kind, items[i].Path = Project, it.Path
				}
				continue
			}
			byName[key] = len(items)
			items = append(items, it)
		}
	}
	for i := range items {
		if items[i].Here() {
			items[i].LastUsed = later(items[i].LastUsed, used[items[i].Name])
		}
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

// Remotes lists the remote workspaces ws ssh has connected to, named
// host or host:target. Read from state only, so the picker never waits
// on the network.
func Remotes(st state.Store) Source {
	return func() ([]Item, error) {
		remotes, err := st.Remotes()
		var out []Item
		for _, r := range remotes {
			name := r.Host
			if r.Target != "" {
				name += ":" + r.Target
			}
			out = append(out, Item{Name: name, Kind: Remote, LastUsed: r.At, Host: r.Host, Target: r.Target})
		}
		return out, err
	}
}

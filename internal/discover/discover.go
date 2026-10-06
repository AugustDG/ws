// Package discover lists what the picker can open: running sessions,
// configured projects, and the workspaces of other machines reached with
// ws ssh. Each origin is a Source; Collect merges them.
package discover

import (
	"encoding/json"
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
	Remote // a workspace on another host, known only by what ws ssh was given
	Host   // a machine; the picker's header row for its items
)

func (k Kind) String() string {
	switch k {
	case Project:
		return "project"
	case Remote:
		return "remote"
	case Host:
		return "host"
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

	// Machine is where the item lives: empty for this machine, "local"
	// for the machine ws ssh connected from (as a host sees it), else the
	// host's ssh name. The picker groups by it.
	Machine string `json:",omitempty"`
	// Host and Target are what ws ssh is given to open the item.
	Host   string `json:",omitempty"`
	Target string `json:",omitempty"`
	// Via is set on items that came over the link from the machine ws ssh
	// connected from ("local"); opening one goes back through it.
	Via string `json:",omitempty"`
	// Cached is set on items a host reported earlier, so their state is
	// as of that host's LastUsed.
	Cached bool `json:",omitempty"`
}

// Label is the kind as shown where items aren't grouped: "patchwork
// session" for an item of another machine.
func (it Item) Label() string {
	if it.Machine != "" && (it.Kind == Session || it.Kind == Project) {
		return it.Machine + " " + it.Kind.String()
	}
	return it.Kind.String()
}

// Here reports whether the item belongs to this machine.
func (it Item) Here() bool { return it.Machine == "" }

// State is running, external (running but not started by ws) or stopped.
// For another machine's item, it's as last reported, or "remote" when the
// state isn't known.
func (it Item) State() string {
	switch {
	case it.Kind == Remote || it.Kind == Host:
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
// follow, running ones first, otherwise in source order. Items merge by
// name only within one machine, and used applies to this machine alone.
func Collect(used map[string]time.Time, sources ...Source) []Item {
	var items []Item
	byName := map[string]int{}
	for _, src := range sources {
		found, _ := src()
		for _, it := range found {
			key := it.Name
			if !it.Here() || it.Kind == Host {
				key = it.Machine + "\x00" + it.Via + "\x00" + it.Name
				if it.Kind == Host || it.Kind == Remote {
					key += "\x00" + it.Kind.String()
				}
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

// Remotes lists the hosts ws ssh has connected to: a Host item for each,
// and the workspaces each last reported (see state.Store.SaveHostItems).
// A host that never reported lists the targets ws ssh was given instead.
// Read from state only, so the picker never waits on the network.
func Remotes(st state.Store) Source {
	return func() ([]Item, error) {
		remotes, err := st.Remotes()
		if err != nil {
			return nil, err
		}
		reports, err := st.HostItems()
		var out []Item
		seen := map[string]bool{}
		for _, r := range remotes {
			_, hasReport := reports[r.Host]
			if !seen[r.Host] {
				seen[r.Host] = true
				out = append(out, Item{Name: r.Host, Kind: Host, Machine: r.Host, Host: r.Host, LastUsed: r.At})
				out = append(out, reported(r.Host, reports[r.Host])...)
			}
			if r.Target != "" && !hasReport {
				out = append(out, Item{Name: r.Target, Kind: Remote, Machine: r.Host, Host: r.Host, Target: r.Target, LastUsed: r.At})
			}
		}
		return out, err
	}
}

// reported decodes a host's report into its items. Their LastUsed is the
// host's own, so they sort with their host.
func reported(host string, r state.HostReport) []Item {
	var items []Item
	if json.Unmarshal(r.Items, &items) != nil {
		return nil
	}
	for i := range items {
		items[i].Machine, items[i].Host, items[i].Target = host, host, items[i].Name
		items[i].Via, items[i].Cached = "", true
		items[i].LastUsed = r.At
	}
	return items
}

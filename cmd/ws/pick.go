package main

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/AugustDG/ws/internal/discover"
	"github.com/AugustDG/ws/internal/picker"
	"github.com/AugustDG/ws/internal/remote"
	"github.com/AugustDG/ws/internal/workspace"
)

// runPicker is `ws` with no arguments. Stopping a session from the picker
// reopens it, so several can be stopped in a row.
func runPicker(_ *cobra.Command, _ []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("the picker needs a terminal; use `ws ls --plain` to script it")
	}
	a, err := newApp()
	if err != nil {
		return err
	}
	for {
		choice, err := picker.Run(a.pickerItems(), a.cfg.AbbrevHome, a.pickerOptions())
		if err != nil {
			return err
		}
		switch choice.Action {
		case picker.Open:
			return a.open(choice.Item)
		case picker.Stop:
			if err := a.stop(choice.Item); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

// items is what ws ls shows: running sessions, projects and remote
// workspaces, most recently used first, and on a host reached with ws ssh,
// the connecting machine's.
func (a *app) items() []discover.Item {
	return a.collect(a.linked)
}

// collect merges this machine's items and the hosts' last reports from
// state with more sources.
func (a *app) collect(more ...discover.Source) []discover.Item {
	used := map[string]time.Time{}
	if uses, err := a.mgr.State.Used(); err == nil {
		for name, u := range uses {
			used[name] = u.At
		}
	}
	sources := append([]discover.Source{discover.Sessions(a.tmux), discover.Projects(a.cfg), discover.Remotes(a.mgr.State)}, more...)
	return discover.Collect(used, sources...)
}

// linked lists the items of the machine ws ssh connected to this host
// from, while that connection is up, under a header that goes back there.
func (a *app) linked() ([]discover.Item, error) {
	path := remote.LinkPath(a.mgr.State.Dir)
	if path == "" {
		return nil, nil
	}
	remote.PruneLinks(path)
	items, err := remote.LinkItems(path)
	if len(items) == 0 {
		return nil, err
	}
	back := discover.Item{Name: remote.LocalMachine, Kind: discover.Host, Machine: remote.LocalMachine, Via: remote.LocalMachine}
	return append([]discover.Item{back}, items...), err
}

// here is this machine's own items, which a host reports over the link.
func (a *app) here() []discover.Item {
	return a.forLink(slices.DeleteFunc(a.collect(), func(it discover.Item) bool { return !it.Here() }))
}

// forLink readies items for the other end of the link, which can't
// shorten this machine's home dir in their paths.
func (a *app) forLink(items []discover.Item) []discover.Item {
	for i := range items {
		items[i].Path = a.cfg.AbbrevHome(items[i].Path)
	}
	return items
}

// pickerOptions names this machine's group: "local", or on a host
// reached with ws ssh, the name it was reached by. There, the connecting
// machine's items load while the picker is already up.
func (a *app) pickerOptions() picker.Options {
	if remote.LinkPath(a.mgr.State.Dir) == "" {
		return picker.Options{Here: remote.LocalMachine}
	}
	name := remote.LinkHost(a.mgr.State.Dir)
	if name == "" {
		name, _ = os.Hostname()
	}
	more := func() []discover.Item {
		items, _ := a.linked()
		return items
	}
	return picker.Options{Here: name, Remote: true, More: more, Loading: remote.LocalMachine}
}

// pickerItems is what the picker shows at once: items without the
// connecting machine's, which it loads afterwards (see pickerOptions).
// The session ws runs in goes last, since switching to it does nothing,
// so the top entry is the one you were in before.
func (a *app) pickerItems() []discover.Item {
	items := a.collect()
	current, err := a.tmux.CurrentSession()
	if err != nil {
		return items
	}
	i := slices.IndexFunc(items, func(it discover.Item) bool { return it.Here() && it.Name == current })
	if i < 0 {
		return items
	}
	it := items[i]
	return append(slices.Delete(items, i, i+1), it)
}

// stop stops a session wherever it runs: here, on the machine this host
// was reached from (over the link), or on a host (over ssh). A host's
// header stops every session ws started there, once confirmed.
func (a *app) stop(it discover.Item) error {
	switch {
	case it.Here():
		return a.mgr.Stop(it.Name, workspace.StopOptions{})
	case it.Via != "":
		return remote.LinkStop(remote.LinkPath(a.mgr.State.Dir), it)
	case it.Kind == discover.Host:
		if confirm(fmt.Sprintf("stop every ws session on %s?", it.Host)) != nil {
			return nil // declined: back to the picker
		}
		return a.stopOnHost(it.Host, "", true)
	}
	return a.stopOnHost(it.Host, it.Name, false)
}

// open attaches to a running item, starting a stopped project first. A
// remote item connects with ws ssh, and an item of the machine this host
// was reached from is opened there.
func (a *app) open(it discover.Item) error {
	if it.Via != "" {
		// Detaching ends the ssh session; ws ssh on the other end opens the
		// item, or for the header, goes back to the session it came from.
		if it.Kind != discover.Host || it.Machine != remote.LocalMachine {
			if err := remote.LinkOpen(remote.LinkPath(a.mgr.State.Dir), it); err != nil {
				return err
			}
		}
		_, err := a.tmux.Run("detach-client")
		return err
	}
	if !it.Here() {
		return a.connect(it.Host, it.Target)
	}
	if !it.Running {
		p, err := a.cfg.Project(it.Name)
		if err != nil {
			return err
		}
		if err := a.start(target{project: p}, ""); err != nil {
			return err
		}
	}
	return a.tmux.Attach(it.Name)
}

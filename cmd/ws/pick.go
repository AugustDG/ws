package main

import (
	"errors"
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
		choice, err := picker.Run(a.pickerItems(), a.cfg.AbbrevHome)
		if err != nil {
			return err
		}
		switch choice.Action {
		case picker.Open:
			return a.open(choice.Item)
		case picker.Stop:
			if err := a.mgr.Stop(choice.Item.Name, workspace.StopOptions{}); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

// items is what the picker and ws ls show: running sessions, projects
// and remote workspaces, most recently used first.
func (a *app) items() []discover.Item {
	used := map[string]time.Time{}
	if uses, err := a.mgr.State.Used(); err == nil {
		for name, u := range uses {
			used[name] = u.At
		}
	}
	return discover.Collect(used, discover.Sessions(a.tmux), discover.Projects(a.cfg), discover.Remotes(a.mgr.State), a.linked)
}

// linked lists the items of the machine ws ssh connected to this host
// from, while that connection is up.
func (a *app) linked() ([]discover.Item, error) {
	path := remote.LinkPath(a.mgr.State.Dir)
	if path == "" {
		return nil, nil
	}
	remote.PruneLinks(path)
	return remote.LinkItems(path)
}

// pickerItems is items with the session ws runs in moved to the end, since
// switching to it does nothing. The top entry is then the one you were in
// before.
func (a *app) pickerItems() []discover.Item {
	items := a.items()
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

// open attaches to a running item, starting a stopped project first. A
// remote item connects with ws ssh, and an item of the machine this host
// was reached from is opened there.
func (a *app) open(it discover.Item) error {
	if it.Via != "" {
		// Detaching ends the ssh session; ws ssh on the other end opens it.
		if err := remote.LinkOpen(remote.LinkPath(a.mgr.State.Dir), it); err != nil {
			return err
		}
		_, err := a.tmux.Run("detach-client")
		return err
	}
	if it.Kind == discover.Remote {
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

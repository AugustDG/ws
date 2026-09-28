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

// items is what the picker and ws ls show: running sessions and projects,
// most recently used first.
func (a *app) items() []discover.Item {
	used := map[string]time.Time{}
	if uses, err := a.mgr.State.Used(); err == nil {
		for name, u := range uses {
			used[name] = u.At
		}
	}
	return discover.Collect(used, discover.Sessions(a.tmux), discover.Projects(a.cfg))
}

// pickerItems is items without the session ws runs in, since there's no
// point switching to it. The top entry is then the one you were in before.
func (a *app) pickerItems() []discover.Item {
	items := a.items()
	if current, err := a.tmux.CurrentSession(); err == nil {
		items = slices.DeleteFunc(items, func(it discover.Item) bool { return it.Name == current })
	}
	return items
}

// open attaches to a running item, starting a stopped project first.
func (a *app) open(it discover.Item) error {
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

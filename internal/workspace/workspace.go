// Package workspace starts and stops project sessions. It ties together
// project config, worktree sources, the engine and persisted state.
package workspace

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"

	"github.com/AugustDG/ws/internal/engine"
	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/state"
	"github.com/AugustDG/ws/internal/tmux"
	"github.com/AugustDG/ws/internal/worktree"
)

// Manager holds the dependencies start and stop need.
type Manager struct {
	Config *project.Config
	Tmux   *tmux.Client
	State  state.Store
	Engine *engine.Engine
	Log    io.Writer // progress and hook output
}

// StartOptions tweak one start.
type StartOptions struct {
	Layout string // replaces the project's layout
}

// Start brings a project's session up, or fills in missing windows if it's
// already running.
func (m *Manager) Start(p project.Project, opts StartOptions) (engine.Result, error) {
	windows, err := m.Config.Windows(p, opts.Layout)
	if err != nil {
		return engine.Result{}, err
	}
	running := m.Tmux.HasSession(p.Name)

	wts, added, err := m.acquire(p, running)
	if err != nil {
		return engine.Result{}, err
	}
	// If a new session fails to come up, give back what this call leased.
	undo := func() {
		if !running {
			m.releaseSome(p.Name, added)
		}
	}

	resolved, err := layout.Expand(windows, layout.Context{
		Project: p.Name, Root: p.Root, HomeDir: m.Config.Home, Worktrees: wts,
	})
	if err != nil {
		undo()
		return engine.Result{}, err
	}
	if !running && p.OnStart != "" {
		if err := m.hook("on_start", p.OnStart, p.Root); err != nil {
			undo()
			return engine.Result{}, err
		}
	}

	res, err := m.Engine.Up(engine.Session{Name: p.Name, Env: p.Env, Windows: resolved})
	if err != nil {
		undo()
	}
	return res, err
}

// acquire gets the project's worktrees, main checkout first, and records
// them. added is what this call leased on top of the existing record.
//
// A running session never shrinks: its panes may still use every worktree
// it has, so lowering count only takes effect on the next fresh start.
func (m *Manager) acquire(p project.Project, running bool) (wts []layout.Worktree, added []worktree.Lease, err error) {
	wts = []layout.Worktree{{Name: "main", Path: p.Root}}
	if p.Worktrees == nil {
		return wts, nil, nil
	}
	src, err := worktree.Get(p.Worktrees.Source)
	if err != nil {
		return nil, nil, err
	}
	rec, err := m.State.Load(p.Name)
	if err != nil {
		return nil, nil, err
	}

	count := p.Worktrees.Count
	if running && len(rec.Leases) > count {
		fmt.Fprintf(m.Log, "%s is running with %d worktrees; count %d applies after ws stop\n", p.Name, len(rec.Leases), count)
		count = len(rec.Leases)
	}
	leases, err := src.Acquire(worktree.Request{
		Root: p.Root, Count: count, Holder: "ws:" + p.Name, Held: rec.Leases,
	})
	// Sources return every lease still worth tracking, even on error.
	if saveErr := m.State.Save(p.Name, state.Session{Root: p.Root, Source: p.Worktrees.Source, Leases: leases}); saveErr != nil {
		err = errors.Join(err, saveErr)
	}
	if err != nil {
		return nil, nil, err
	}

	sub := worktree.SubdirOf(p.Root)
	for _, l := range leases {
		wts = append(wts, layout.Worktree{Name: l.Name, Path: filepath.Join(l.Path, sub)})
		if !slices.Contains(rec.Leases, l) {
			added = append(added, l)
		}
	}
	return wts, added, nil
}

// StopOptions tweak one stop.
type StopOptions struct {
	KeepWorktrees bool // keep leases so the next start reuses them
}

// Stop kills the session, runs on_stop if it was running and gives back
// its worktrees. Stopping a session that's already gone still returns
// any worktrees recorded for it.
func (m *Manager) Stop(name string, opts StopOptions) error {
	var errs []error
	if m.Tmux.HasSession(name) {
		if err := m.Tmux.KillSession(name); err != nil {
			return err
		}
		if p, err := m.Config.Project(name); err == nil && p.OnStop != "" {
			errs = append(errs, m.hook("on_stop", p.OnStop, p.Root))
		}
	}
	if !opts.KeepWorktrees {
		errs = append(errs, m.releaseAll(name))
	}
	return errors.Join(errs...)
}

// releaseAll gives back every lease recorded for the session and forgets it.
func (m *Manager) releaseAll(name string) error {
	rec, err := m.State.Load(name)
	if err != nil {
		return err
	}
	return m.releaseSome(name, rec.Leases)
}

// releaseSome gives back leases and drops them from the session's record.
// Leases that fail to return stay recorded so a later stop can retry.
func (m *Manager) releaseSome(name string, leases []worktree.Lease) error {
	rec, err := m.State.Load(name)
	if err != nil {
		return err
	}
	var releaseErr error
	if len(leases) > 0 {
		src, err := worktree.Get(rec.Source)
		if err != nil {
			return err
		}
		kept, err := src.Release(rec.Root, leases)
		if err != nil {
			releaseErr = fmt.Errorf("returning worktrees (the rest stay recorded; run ws stop %s again): %w", name, err)
		}
		rec.Leases = slices.DeleteFunc(rec.Leases, func(l worktree.Lease) bool {
			return slices.Contains(leases, l) && !slices.Contains(kept, l)
		})
	}
	return errors.Join(releaseErr, m.saveOrDelete(name, rec))
}

func (m *Manager) saveOrDelete(name string, rec state.Session) error {
	if len(rec.Leases) == 0 {
		return m.State.Delete(name)
	}
	return m.State.Save(name, rec)
}

// Leases returns what's recorded for a session.
func (m *Manager) Leases(name string) ([]worktree.Lease, error) {
	rec, err := m.State.Load(name)
	return rec.Leases, err
}

func (m *Manager) hook(name, script, dir string) error {
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = m.Log, m.Log
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s hook: %w", name, err)
	}
	return nil
}

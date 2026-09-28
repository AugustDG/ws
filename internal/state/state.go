// Package state persists what ws needs across runs: what it acquired for
// each session, so stop can give back exactly that, and the workspace a
// client was last in.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/AugustDG/ws/internal/worktree"
)

// Session is the record for one session name.
type Session struct {
	Root   string           `json:"root"`
	Source string           `json:"source,omitempty"`
	Leases []worktree.Lease `json:"leases,omitempty"`
}

// Store keeps one JSON file per session in Dir.
type Store struct{ Dir string }

// Default uses $WS_STATE_DIR, else $XDG_STATE_HOME/ws, else
// ~/.local/state/ws.
func Default() (Store, error) {
	if d := os.Getenv("WS_STATE_DIR"); d != "" {
		return Store{Dir: d}, nil
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return Store{Dir: filepath.Join(d, "ws")}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Dir: filepath.Join(home, ".local", "state", "ws")}, nil
}

func (s Store) path(name string) string { return filepath.Join(s.Dir, name+".json") }

// Load returns the record for name, or an empty one if there is none.
func (s Store) Load(name string) (Session, error) {
	var sess Session
	data, err := os.ReadFile(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return sess, nil
	}
	if err != nil {
		return sess, err
	}
	return sess, json.Unmarshal(data, &sess)
}

func (s Store) Save(name string, sess Session) error {
	return s.write(s.path(name), sess)
}

// write stores v as JSON at path, replacing it atomically.
func (s Store) write(path string, v any) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	// A unique temp file, since tmux hooks can write the same record at once.
	f, err := os.CreateTemp(s.Dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

func (s Store) Delete(name string) error {
	err := os.Remove(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Names lists the sessions that have a record.
func (s Store) Names() ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = strings.TrimSuffix(filepath.Base(p), ".json")
	}
	return names, nil
}

// Last is the workspace a tmux client was most recently in.
type Last struct {
	Name string `json:"name"`
	Root string `json:"root,omitempty"`
}

// lastPath has no .json extension so Names doesn't list it as a session.
func (s Store) lastPath() string { return filepath.Join(s.Dir, "last") }

func (s Store) SaveLast(l Last) error { return s.write(s.lastPath(), l) }

// LoadLast returns the last workspace, and false if none is recorded.
func (s Store) LoadLast() (Last, bool, error) {
	var l Last
	data, err := os.ReadFile(s.lastPath())
	if errors.Is(err, os.ErrNotExist) {
		return l, false, nil
	}
	if err != nil {
		return l, false, err
	}
	return l, true, json.Unmarshal(data, &l)
}

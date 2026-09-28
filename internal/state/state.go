// Package state persists what ws acquired for a running session, so stop
// can give back exactly that.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

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
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(name) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

func (s Store) Delete(name string) error {
	err := os.Remove(s.path(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Package state persists what ws needs across runs: what it acquired for
// each session, so stop can give back exactly that, when each workspace
// was last used, and which remote workspaces ws ssh reached.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

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

// Use is when a client was last in a workspace, and where it was rooted.
type Use struct {
	Root string    `json:"root,omitempty"`
	At   time.Time `json:"at"`
}

// usedPath has no .json extension so Names doesn't list it as a session.
func (s Store) usedPath() string { return filepath.Join(s.Dir, "used") }

// Used returns when each workspace was last used, by session name.
func (s Store) Used() (map[string]Use, error) {
	used := map[string]Use{}
	data, err := os.ReadFile(s.usedPath())
	if errors.Is(err, os.ErrNotExist) {
		return used, nil
	}
	if err != nil {
		return nil, err
	}
	return used, json.Unmarshal(data, &used)
}

// RecordUse stamps name as used now. tmux hooks can record at the same
// moment, so the read-modify-write holds a lock.
func (s Store) RecordUse(name, root string) error {
	return s.locked(s.usedPath(), func() error {
		used, err := s.Used()
		if err != nil {
			return err
		}
		used[name] = Use{Root: root, At: time.Now()}
		return s.write(s.usedPath(), used)
	})
}

// locked runs fn holding an exclusive lock on path's lock file.
func (s Store) locked(path string, fn func() error) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	return fn()
}

// Remote is a workspace reached with ws ssh: a host and the target given
// there, if any.
type Remote struct {
	Host   string    `json:"host"`
	Target string    `json:"target,omitempty"`
	At     time.Time `json:"at"`
}

// remotesPath has no .json extension so Names doesn't list it as a session.
func (s Store) remotesPath() string { return filepath.Join(s.Dir, "remotes") }

// Remotes lists the remote workspaces connected to, newest first. They're
// kept apart from Used so ws last stays on this machine.
func (s Store) Remotes() ([]Remote, error) {
	var remotes []Remote
	data, err := os.ReadFile(s.remotesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return remotes, json.Unmarshal(data, &remotes)
}

// RecordRemote stamps host and target as connected to now.
func (s Store) RecordRemote(host, target string) error {
	return s.locked(s.remotesPath(), func() error {
		remotes, err := s.Remotes()
		if err != nil {
			return err
		}
		remotes = slices.DeleteFunc(remotes, func(r Remote) bool { return r.Host == host && r.Target == target })
		remotes = append([]Remote{{Host: host, Target: target, At: time.Now()}}, remotes...)
		return s.write(s.remotesPath(), remotes)
	})
}

// Last returns the most recently used workspace, and false if none is.
func (s Store) Last() (string, Use, bool, error) {
	used, err := s.Used()
	var name string
	var last Use
	for n, u := range used {
		if u.At.After(last.At) {
			name, last = n, u
		}
	}
	return name, last, name != "", err
}

// HostReport is what a host last reported of its workspaces over the
// link, as discover items in JSON (state can't import discover).
type HostReport struct {
	At    time.Time       `json:"at"`
	Items json.RawMessage `json:"items"`
}

// hostsPath has no .json extension so Names doesn't list it as a session.
func (s Store) hostsPath() string { return filepath.Join(s.Dir, "hosts") }

// HostItems returns each host's last report, by host.
func (s Store) HostItems() (map[string]HostReport, error) {
	reports := map[string]HostReport{}
	data, err := os.ReadFile(s.hostsPath())
	if errors.Is(err, os.ErrNotExist) {
		return reports, nil
	}
	if err != nil {
		return nil, err
	}
	return reports, json.Unmarshal(data, &reports)
}

// SaveHostItems replaces host's report with items, stamped now.
func (s Store) SaveHostItems(host string, items any) error {
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return s.locked(s.hostsPath(), func() error {
		reports, err := s.HostItems()
		if err != nil {
			return err
		}
		reports[host] = HostReport{At: time.Now(), Items: data}
		return s.write(s.hostsPath(), reports)
	})
}

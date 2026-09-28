package worktree

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
)

// Treehouse leases worktrees from a treehouse pool. Leases are durable:
// treehouse won't hand them out or prune them until they're returned.
type Treehouse struct{}

// treehouseEntry is the part of `treehouse get --lease --json` and
// `treehouse status --json` entries that ws reads.
type treehouseEntry struct {
	Path    string `json:"path"`
	LeaseID string `json:"lease_id"`
}

func (Treehouse) Acquire(req Request) ([]Lease, error) {
	count := max(req.Count, 1)

	valid, err := stillOurs(req.Root, req.Held)
	if err != nil {
		// Can't tell which leases are still ours; keep tracking all of them.
		return req.Held, err
	}
	if len(valid) > count {
		kept, err := (Treehouse{}).Release(req.Root, valid[count:])
		valid = slices.Concat(valid[:count], kept)
		if err != nil {
			return valid, err
		}
	}

	for len(valid) < count {
		out, err := run(req.Root, "treehouse", "get", "--lease", "--json", "--lease-holder", req.Holder)
		if err != nil {
			return valid, err
		}
		var e treehouseEntry
		if err := json.Unmarshal([]byte(out), &e); err != nil {
			return valid, fmt.Errorf("treehouse get: unexpected output %q", out)
		}
		valid = append(valid, Lease{Name: slotName(e.Path), Path: e.Path, ID: e.LeaseID})
	}
	return valid, nil
}

// stillOurs filters held leases down to the ones treehouse still records
// under the same lease id. Others were returned or re-leased elsewhere, so
// they're dropped without being returned.
func stillOurs(root string, held []Lease) ([]Lease, error) {
	if len(held) == 0 {
		return nil, nil
	}
	out, err := run(root, "treehouse", "status", "--json")
	if err != nil {
		return nil, err
	}
	var entries []treehouseEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("treehouse status: unexpected output")
	}
	current := map[string]string{}
	for _, e := range entries {
		current[e.Path] = e.LeaseID
	}
	var valid []Lease
	for _, l := range held {
		if id, ok := current[l.Path]; ok && id == l.ID {
			valid = append(valid, l)
		}
	}
	return valid, nil
}

// Release returns the leases that are still ours. Ones treehouse no longer
// records under our lease id were returned or re-leased elsewhere, so
// they're dropped rather than returned.
func (Treehouse) Release(root string, leases []Lease) ([]Lease, error) {
	if len(leases) == 0 {
		return nil, nil
	}
	ours, err := stillOurs(root, leases)
	if err != nil {
		return leases, err
	}
	var kept []Lease
	var errs []error
	for _, l := range ours {
		if _, err := run(root, "treehouse", "return", "--if-lease-id", l.ID, l.Path); err != nil {
			kept = append(kept, l)
			errs = append(errs, fmt.Errorf("%s: %w", l.Path, err))
		}
	}
	return kept, errors.Join(errs...)
}

// slotName is the pool slot a worktree lives in: <pool>/<slot>/<repo>.
func slotName(path string) string {
	return filepath.Base(filepath.Dir(path))
}

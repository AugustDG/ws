package workspace

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AugustDG/ws/internal/engine"
	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/state"
	"github.com/AugustDG/ws/internal/tmux"
)

// fakeTreehouse is a stand-in for the treehouse CLI. Slots live in
// $FAKE_TH/N; a slot's "lease" file holds its lease id. $FAKE_TH_FAIL makes
// the named subcommand fail.
const fakeTreehouse = `#!/bin/sh
set -e
[ "$1" = "$FAKE_TH_FAIL" ] && { echo "$1 failed" >&2; exit 1; }
case "$1" in
status)
  printf '['; sep=''
  for d in "$FAKE_TH"/*/; do
    [ -d "$d" ] || continue
    id=$(cat "$d/lease" 2>/dev/null || true)
    printf '%s{"path":"%s","lease_id":"%s"}' "$sep" "${d%/}" "$id"; sep=','
  done
  echo ']' ;;
get)
  n=1; while [ -s "$FAKE_TH/$n/lease" ]; do n=$((n+1)); done
  mkdir -p "$FAKE_TH/$n"; id="lease-$n-$$"; echo "$id" > "$FAKE_TH/$n/lease"
  printf '{"path":"%s","lease_id":"%s"}\n' "$FAKE_TH/$n" "$id" ;;
return)
  [ "$(cat "$4/lease")" = "$3" ] || { echo "lease precondition failed" >&2; exit 1; }
  : > "$4/lease" ;;
esac
`

type fixture struct {
	t    *testing.T
	m    *Manager
	pool string
	p    project.Project
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	bin, pool := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "treehouse"), []byte(fakeTreehouse), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("FAKE_TH", pool)
	t.Setenv("TMUX", "")

	tc := &tmux.Client{Bin: "tmux", Socket: fmt.Sprintf("ws-ws-test-%d", os.Getpid())}
	t.Cleanup(func() { tc.Run("kill-server") })
	root := t.TempDir()
	return &fixture{
		t:    t,
		pool: pool,
		m: &Manager{
			Config: &project.Config{Dir: t.TempDir(), Home: root},
			Tmux:   tc,
			State:  state.Store{Dir: t.TempDir()},
			Engine: &engine.Engine{Tmux: tc, Width: 120, Height: 40},
			Log:    io.Discard,
		},
		p: project.Project{
			Name:      "proj",
			Root:      root,
			Windows:   []layout.Window{{Name: "code", Node: layout.Node{Columns: []layout.Node{{ForEach: layout.ForEachWorktree}}}}},
			Worktrees: &project.Worktrees{Source: "treehouse", Count: 2},
		},
	}
}

// leased lists slots whose lease file is non-empty.
func (f *fixture) leased() []string {
	var out []string
	entries, _ := os.ReadDir(f.pool)
	for _, e := range entries {
		if data, _ := os.ReadFile(filepath.Join(f.pool, e.Name(), "lease")); len(strings.TrimSpace(string(data))) > 0 {
			out = append(out, e.Name())
		}
	}
	return out
}

func (f *fixture) recorded() int {
	rec, err := f.m.State.Load(f.p.Name)
	if err != nil {
		f.t.Fatal(err)
	}
	return len(rec.Leases)
}

func TestStartStopLeasesAndReturns(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Start(f.p, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.leased(); len(got) != 2 || f.recorded() != 2 {
		t.Fatalf("leased %v, recorded %d", got, f.recorded())
	}
	rows, _ := f.m.Tmux.Lines("list-panes", "-t", "=proj:code")
	if len(rows) != 3 {
		t.Errorf("want 3 columns (root + 2 worktrees), got %d", len(rows))
	}

	if err := f.m.Stop(f.p.Name, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.leased(); len(got) != 0 || f.recorded() != 0 {
		t.Errorf("after stop: leased %v, recorded %d", got, f.recorded())
	}
}

func TestRunningSessionNeverShrinks(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Start(f.p, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	f.p.Worktrees.Count = 1
	if _, err := f.m.Start(f.p, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := f.leased(); len(got) != 2 {
		t.Errorf("running session lost a worktree: leased %v", got)
	}
}

func TestKeepWorktreesReusesLeases(t *testing.T) {
	f := newFixture(t)
	f.m.Start(f.p, StartOptions{})
	before, _ := f.m.Leases(f.p.Name)
	if err := f.m.Stop(f.p.Name, StopOptions{KeepWorktrees: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Start(f.p, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	after, _ := f.m.Leases(f.p.Name)
	if len(after) != 2 || after[0] != before[0] || after[1] != before[1] {
		t.Errorf("leases changed: %v -> %v", before, after)
	}
}

func TestTreehouseErrorKeepsRecord(t *testing.T) {
	f := newFixture(t)
	f.m.Start(f.p, StartOptions{})
	f.m.Stop(f.p.Name, StopOptions{KeepWorktrees: true})

	t.Setenv("FAKE_TH_FAIL", "status")
	if _, err := f.m.Start(f.p, StartOptions{}); err == nil {
		t.Fatal("expected the status failure to surface")
	}
	if f.recorded() != 2 {
		t.Errorf("record lost on error: %d leases", f.recorded())
	}
	if err := f.m.Stop(f.p.Name, StopOptions{}); err == nil {
		t.Error("stop should fail while treehouse is down")
	}
	if f.recorded() != 2 {
		t.Errorf("record lost on failed stop: %d leases", f.recorded())
	}

	t.Setenv("FAKE_TH_FAIL", "")
	if err := f.m.Stop(f.p.Name, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(f.leased()) != 0 || f.recorded() != 0 {
		t.Errorf("retry didn't clean up: leased %v", f.leased())
	}
}

func TestFailedStartReturnsNewLeases(t *testing.T) {
	f := newFixture(t)
	f.p.Windows = []layout.Window{{Name: "bad", Layout: "no-such-preset", Panes: []layout.Node{{}, {}}}}
	if _, err := f.m.Start(f.p, StartOptions{}); err == nil {
		t.Fatal("expected the bad preset to fail")
	}
	if f.m.Tmux.HasSession(f.p.Name) {
		t.Error("half-built session left running")
	}
	if len(f.leased()) != 0 || f.recorded() != 0 {
		t.Errorf("leases leaked: leased %v, recorded %d", f.leased(), f.recorded())
	}
}

package remote

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/AugustDG/ws/internal/discover"
)

// sockDir is a temp dir short enough for Unix socket paths.
func sockDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wsl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestLink(t *testing.T) {
	items := []discover.Item{
		{Name: "api", Kind: discover.Session, Running: true},
		{Name: "box", Kind: discover.Remote, Host: "box"},
	}
	path := filepath.Join(sockDir(t), "l.sock")
	var pushed []discover.Item
	link, err := Listen(path, func() []discover.Item { return items }, func(it []discover.Item) { pushed = it })
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()

	got, err := LinkItems(path)
	if err != nil || len(got) != 2 {
		t.Fatalf("items %+v, %v", got, err)
	}
	if got[0].Via != "local" || got[0].Label() != "local session" || got[1].Label() != "remote" {
		t.Errorf("labels %q %q", got[0].Label(), got[1].Label())
	}
	if _, ok := link.Chosen(); ok {
		t.Fatal("chosen before anything was picked")
	}
	if err := LinkOpen(path, got[1]); err != nil {
		t.Fatal(err)
	}
	it, ok := link.Chosen()
	if !ok || it.Name != "box" || it.Via != "" || it.Host != "box" {
		t.Fatalf("chosen %+v %v", it, ok)
	}
	if _, ok := link.Chosen(); ok {
		t.Error("Chosen didn't clear")
	}
	if err := LinkPush(path, []discover.Item{{Name: "srv", Kind: discover.Session}}); err != nil {
		t.Fatal(err)
	}
	if len(pushed) != 1 || pushed[0].Name != "srv" {
		t.Errorf("pushed %+v", pushed)
	}
	link.Close()
	if _, err := LinkItems(path); err == nil {
		t.Error("a closed link still answered")
	}
}

func TestPruneLinks(t *testing.T) {
	dir := sockDir(t)
	live := filepath.Join(dir, "ws-link-live.sock")
	ln, err := net.Listen("unix", live)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	dead := filepath.Join(dir, "ws-link-dead.sock")
	keep := filepath.Join(dir, "ws-link-keep.sock")
	for _, p := range []string{dead, keep} {
		l, err := net.Listen("unix", p)
		if err != nil {
			t.Fatal(err)
		}
		l.(*net.UnixListener).SetUnlinkOnClose(false)
		l.Close()
	}
	pruneLinks(filepath.Join(dir, "ws-link-*.sock"), keep)
	for p, want := range map[string]bool{live: true, keep: true, dead: false} {
		if exists(p) != want {
			t.Errorf("%s exists = %v, want %v", filepath.Base(p), !want, want)
		}
	}
}

func TestScriptRecordsLink(t *testing.T) {
	bin := fakeBin(t, "tmux")
	home := t.TempDir()
	cmd := Script("", "/tmp/ws-link-abc.sock", "box")
	out := hostShellPath(t, home, bin, cmd)
	if out != "tmux\nnew-session\n-A\n-s\nmain\n" {
		t.Errorf("ran %q", out)
	}
	state := filepath.Join(home, ".local/state/ws")
	if path, host := LinkPath(state), LinkHost(state); path != "/tmp/ws-link-abc.sock" || host != "box" {
		t.Errorf("recorded %q %q", path, host)
	}
}

package remote

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// hostShell runs a script as if on a host whose home is a temp dir. Its
// tmux socket dir is private, so nothing reaches a real tmux server.
func hostShell(t *testing.T, home, script string, stdin []byte) string {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "TMUX_TMPDIR=" + t.TempDir()}
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	return string(out)
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestProbe(t *testing.T) {
	home := t.TempDir()
	p, err := ParseProbe(hostShell(t, home, ProbeScript([]string{"no-such-term-ws"}), nil))
	if err != nil {
		t.Fatal(err)
	}
	if p.OS != runtime.GOOS || p.Arch != runtime.GOARCH {
		t.Errorf("platform %s/%s, want %s/%s", p.OS, p.Arch, runtime.GOOS, runtime.GOARCH)
	}
	if p.WSHash != "" || p.TmuxStamp != "" || p.TmuxConfig || p.WSConfig {
		t.Errorf("empty home probed as %+v", p)
	}
	if !slices.Equal(p.MissingTerms, []string{"no-such-term-ws"}) {
		t.Errorf("missing terms %q", p.MissingTerms)
	}

	write(t, filepath.Join(home, ".local/bin/ws"), "binary")
	write(t, filepath.Join(home, ".config/tmux", StampFile), "abc")
	write(t, filepath.Join(home, ".config/ws/layouts/x.yaml"), "")
	p, err = ParseProbe(hostShell(t, home, ProbeScript(nil), nil))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := FileHash(filepath.Join(home, ".local/bin/ws"))
	if p.WSHash != want || p.TmuxStamp != "abc" || !p.TmuxConfig || !p.WSConfig || p.WSStamp != "" {
		t.Errorf("probed %+v", p)
	}
}

func TestParseProbeRejectsJunk(t *testing.T) {
	if _, err := ParseProbe("Welcome to the host!\n"); err == nil {
		t.Fatal("accepted output without os/arch")
	}
}

func TestNewPlan(t *testing.T) {
	base := Probe{OS: "linux", Arch: "amd64", Tmux: "tmux 3.4", Tic: true, PkgManager: "apt-get", Sudo: true}
	local := Local{WSHash: "w1", TmuxHash: "t1", WSConfHash: "c1", Terms: []string{"xterm-ghostty", "tmux-256color"}}

	t.Run("fresh host", func(t *testing.T) {
		p := base
		p.Tmux = ""
		p.MissingTerms = []string{"xterm-ghostty", "unknown-here"}
		plan := NewPlan(p, local, false)
		for _, s := range plan.Steps() {
			if !s.Do || s.Fail {
				t.Errorf("%s: %+v", s.Name, s)
			}
		}
		if !slices.Equal(plan.Terms, []string{"xterm-ghostty"}) {
			t.Errorf("terms %q", plan.Terms)
		}
		if !strings.HasPrefix(plan.TmuxInstall, "sudo apt-get update") {
			t.Errorf("install %q", plan.TmuxInstall)
		}
	})
	t.Run("up to date", func(t *testing.T) {
		p := base
		p.WSHash, p.TmuxStamp, p.WSStamp, p.TmuxConfig, p.WSConfig = "w1", "t1", "c1", true, true
		for _, s := range NewPlan(p, local, false).Steps() {
			if s.Do || s.Fail {
				t.Errorf("%s: %+v", s.Name, s)
			}
		}
	})
	t.Run("changed since last setup", func(t *testing.T) {
		p := base
		p.WSHash, p.TmuxStamp, p.WSStamp, p.TmuxConfig, p.WSConfig = "w0", "t0", "c0", true, true
		plan := NewPlan(p, local, false)
		if !plan.WS.Do || !plan.TmuxConf.Do || !plan.WSConf.Do || plan.TmuxConf.Note != "update" {
			t.Errorf("%+v", plan)
		}
	})
	t.Run("host's own config", func(t *testing.T) {
		p := base
		p.TmuxConfig, p.WSConfig = true, true
		plan := NewPlan(p, local, false)
		if plan.TmuxConf.Do || plan.WSConf.Do || !strings.Contains(plan.TmuxConf.Note, "--force") {
			t.Errorf("replaced without force: %+v", plan)
		}
		plan = NewPlan(p, local, true)
		if !plan.TmuxConf.Do || !plan.WSConf.Do {
			t.Errorf("force didn't replace: %+v", plan)
		}
	})
	t.Run("nothing local", func(t *testing.T) {
		plan := NewPlan(base, Local{WSHash: "w1"}, false)
		if plan.TmuxConf.Do || plan.WSConf.Do {
			t.Errorf("%+v", plan)
		}
	})
	t.Run("can't install", func(t *testing.T) {
		p := base
		p.Tmux, p.Sudo, p.Arch, p.Tic = "", false, "riscv64", false
		p.MissingTerms = []string{"xterm-ghostty"}
		plan := NewPlan(p, local, false)
		for _, s := range []Step{plan.Tmux, plan.WS, plan.Terminfo} {
			if s.Do || !s.Fail {
				t.Errorf("%s: %+v", s.Name, s)
			}
		}
	})
}

func TestInstallTmux(t *testing.T) {
	cases := []struct {
		p    Probe
		want string
	}{
		{Probe{PkgManager: "dnf", Root: true}, "dnf install -y tmux"},
		{Probe{PkgManager: "apk", Sudo: true}, "sudo apk add tmux"},
		{Probe{PkgManager: "brew"}, "brew install tmux"},
		{Probe{PkgManager: "apt-get", Sudo: true}, "sudo apt-get update -qq && sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq tmux"},
		{Probe{PkgManager: "dnf"}, ""},
		{Probe{Root: true}, ""},
	}
	for _, tc := range cases {
		got, err := InstallTmux(tc.p)
		if got != tc.want || (tc.want == "") != (err != nil) {
			t.Errorf("%+v: got %q, %v; want %q", tc.p, got, err, tc.want)
		}
	}
}

func tarOf(t *testing.T, b Bundle) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := b.WriteTar(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestBundleHash(t *testing.T) {
	dir := t.TempDir()
	b := Bundle{Root: dir}
	if h, err := b.Hash(); h != "" || err != nil {
		t.Fatalf("empty dir hashed to %q, %v", h, err)
	}
	if h, err := (Bundle{Root: filepath.Join(dir, "missing")}).Hash(); h != "" || err != nil {
		t.Fatalf("missing dir hashed to %q, %v", h, err)
	}
	write(t, filepath.Join(dir, "tmux.conf"), "set -g mouse on")
	h1, _ := b.Hash()
	write(t, filepath.Join(dir, ".git/HEAD"), "ref")
	write(t, filepath.Join(dir, StampFile), "x")
	if h, _ := b.Hash(); h != h1 {
		t.Error(".git or the stamp changed the hash")
	}
	write(t, filepath.Join(dir, "tmux.conf"), "set -g mouse off")
	if h, _ := b.Hash(); h == h1 {
		t.Error("content change kept the hash")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	h2, _ := b.Hash()
	if h, _ := (Bundle{Root: link}).Hash(); h != h2 {
		t.Error("a symlinked root hashed differently")
	}
}

func TestSyncTmux(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "tmux.conf"), "new")
	write(t, filepath.Join(src, "plugins/tpm/tpm"), "plugin")
	write(t, filepath.Join(src, "plugins/tpm/.git/HEAD"), "ref")
	write(t, filepath.Join(src, ".git"), "gitdir: ../.git/modules/tmux") // a submodule
	b := Bundle{Root: src}
	hash, _ := b.Hash()

	home := t.TempDir()
	conf := filepath.Join(home, ".config/tmux")
	write(t, filepath.Join(conf, "tmux.conf"), "theirs")
	write(t, filepath.Join(home, ".tmux.conf"), "dotfile")
	hostShell(t, home, SyncTmuxScript(hash), tarOf(t, b))

	if read(t, filepath.Join(conf, "tmux.conf")) != "new" || read(t, filepath.Join(conf, "plugins/tpm/tpm")) != "plugin" {
		t.Fatal("config not installed")
	}
	if exists(filepath.Join(conf, "plugins/tpm/.git")) || exists(filepath.Join(conf, ".git")) {
		t.Error(".git was sent")
	}
	if read(t, filepath.Join(conf, StampFile)) != hash {
		t.Error("stamp not written")
	}
	if read(t, filepath.Join(home, ".config/tmux.ws-bak/tmux.conf")) != "theirs" || read(t, filepath.Join(home, ".tmux.conf.ws-bak")) != "dotfile" {
		t.Error("the host's config wasn't kept")
	}
	if exists(filepath.Join(home, ".tmux.conf")) {
		t.Error("~/.tmux.conf would shadow the synced config")
	}

	// A second sync replaces a dir setup owns without another backup.
	write(t, filepath.Join(src, "tmux.conf"), "newer")
	write(t, filepath.Join(conf, "stale"), "")
	hash2, _ := b.Hash()
	hostShell(t, home, SyncTmuxScript(hash2), tarOf(t, b))
	if read(t, filepath.Join(conf, "tmux.conf")) != "newer" || exists(filepath.Join(conf, "stale")) {
		t.Error("update didn't replace the dir")
	}
	if read(t, filepath.Join(home, ".config/tmux.ws-bak/tmux.conf")) != "theirs" {
		t.Error("update overwrote the backup")
	}
}

func TestSyncWSConfig(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "config.yaml"), "default_layout: grid")
	write(t, filepath.Join(src, "layouts/grid.yaml"), "mine")
	write(t, filepath.Join(src, "projects/local.yaml"), "root: ~/projects/x")
	b := Bundle{Root: src, Paths: WSConfigPaths}
	hash, _ := b.Hash()

	home := t.TempDir()
	d := filepath.Join(home, ".config/ws")
	write(t, filepath.Join(d, "layouts/grid.yaml"), "theirs")
	write(t, filepath.Join(d, "projects/host.yaml"), "root: /srv/app")
	hostShell(t, home, SyncWSConfigScript(hash), tarOf(t, b))

	if read(t, filepath.Join(d, "layouts/grid.yaml")) != "mine" || read(t, filepath.Join(d, "config.yaml")) != "default_layout: grid" {
		t.Error("config not installed")
	}
	if exists(filepath.Join(d, "projects/local.yaml")) {
		t.Error("local projects were sent")
	}
	if !exists(filepath.Join(d, "projects/host.yaml")) {
		t.Error("the host's projects were touched")
	}
	if read(t, filepath.Join(d, "layouts.ws-bak/grid.yaml")) != "theirs" {
		t.Error("the host's layouts weren't kept")
	}
	if read(t, filepath.Join(d, StampFile)) != hash || exists(filepath.Join(d, ".ws-new")) {
		t.Error("stamp missing or staging dir left behind")
	}
}

func TestInstallWS(t *testing.T) {
	home := t.TempDir()
	hostShell(t, home, InstallWSScript, []byte("#!/bin/sh\necho hi\n"))
	path := filepath.Join(home, ".local/bin/ws")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 || read(t, path) != "#!/bin/sh\necho hi\n" {
		t.Fatalf("installed %v %v", info, err)
	}
}

func TestTerminfo(t *testing.T) {
	if _, err := exec.LookPath("tic"); err != nil {
		t.Skip("no tic")
	}
	terms := HaveTerminfo([]string{"xterm-256color", "no-such-term-ws"})
	if !slices.Equal(terms, []string{"xterm-256color"}) {
		t.Skipf("local terminfo has %q", terms)
	}
	src, err := TerminfoSource(terms)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	hostShell(t, home, TerminfoScript, src)
	if !exists(filepath.Join(home, ".terminfo")) {
		t.Fatal("nothing compiled into ~/.terminfo")
	}
}

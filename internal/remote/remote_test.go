package remote

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeBin makes a PATH dir holding sh and, for each name, a script that
// prints its name and arguments one per line.
func fakeBin(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	if err := os.Symlink(sh, filepath.Join(dir, "sh")); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		body := "#!" + sh + "\n"
		if name == "ws-with-last" {
			name = "ws"
			body += `[ "$1" = last ] && { echo proj; exit 0; }` + "\n"
		}
		body += `printf '%s\n' "${0##*/}" "$@"` + "\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runRemote runs the command ssh would send, through a login shell on the
// "host", and returns what it printed and its exit status.
func runRemote(t *testing.T, bin, target string) ([]string, int) {
	t.Helper()
	c := &Conn{Host: "h", Target: target}
	args := c.Args()
	cmd := exec.Command(filepath.Join(bin, "sh"), "-c", args[len(args)-1])
	cmd.Env = []string{"PATH=" + bin, "HOME=" + t.TempDir()}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n"), code
}

func TestScript(t *testing.T) {
	cases := []struct {
		name, target string
		bins         []string
		want         []string
		code         int
	}{
		{"ws with target", "it's here", []string{"ws", "tmux"}, []string{"ws", "start", "it's here"}, 0},
		{"ws reopens last", "", []string{"ws-with-last", "tmux"}, []string{"ws", "start", "proj"}, 0},
		{"ws with nothing recorded", "", []string{"ws", "tmux"}, []string{"ws", "start", "ws\nlast"}, 0},
		{"tmux only", "", []string{"tmux"}, []string{"tmux", "new-session", "-A", "-s", "main"}, 0},
		{"tmux only with target", "dev", []string{"tmux"}, []string{"tmux", "new-session", "-A", "-s", "dev"}, 0},
		{"neither", "", nil, []string{""}, 127},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, code := runRemote(t, fakeBin(t, tc.bins...), tc.target)
			if code != tc.code {
				t.Fatalf("exit %d, want %d", code, tc.code)
			}
			want := strings.Split(strings.Join(tc.want, "\n"), "\n")
			if !slices.Equal(got, want) {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	for _, host := range []string{"", "-oProxyCommand=x"} {
		if err := (&Conn{Host: host}).Validate(); err == nil {
			t.Errorf("host %q accepted", host)
		}
	}
	if err := (&Conn{Host: "user@box"}).Validate(); err != nil {
		t.Error(err)
	}
}

// fakeSSH is an ssh that runs the shell case for its attempt number, so a
// test scripts each connection. `connect` does what ssh does once it has
// logged in: runs the LocalCommand. So `1) connect; exit 255;;` is a
// connection that got in and then dropped.
func fakeSSH(t *testing.T, cases string) (*Conn, func() int) {
	t.Helper()
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	script := `#!/bin/sh
lc=""
while [ $# -gt 0 ]; do
  case $1 in -o) case $2 in LocalCommand=*) lc=${2#LocalCommand=};; esac; shift;; esac
  shift
done
connect() { sh -c "$lc"; }
n=$(cat "` + count + `" 2>/dev/null || echo 0); n=$((n+1)); echo $n > "` + count + `"
case $n in ` + cases + ` esac
`
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Conn{Host: "h", SSH: ssh, Log: &bytes.Buffer{}, Backoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond}
	attempts := func() int {
		b, _ := os.ReadFile(count)
		var n int
		for _, r := range strings.TrimSpace(string(b)) {
			n = n*10 + int(r-'0')
		}
		return n
	}
	return c, attempts
}

func TestRun(t *testing.T) {
	cases := []struct {
		name     string
		ssh      string
		code     int // 0 means Run returns nil
		attempts int
	}{
		{"clean detach", "*) connect; exit 0;;", 0, 1},
		{"never connected", "*) exit 255;;", 255, 1},
		// Slow to give up (an unreachable host, a login that took a while
		// to fail) still isn't a connection that dropped.
		{"slow failure", "*) sleep 0.3; exit 255;;", 255, 1},
		{"remote failure", "*) connect; exit 127;;", 127, 1},
		{"drop then back", "1) connect; exit 255;; *) connect; exit 0;;", 0, 2},
		{"keeps retrying while down", "1) connect; exit 255;; 2|3) exit 255;; *) connect; exit 0;;", 0, 4},
		{"second drop", "1|2) connect; exit 255;; *) connect; exit 0;;", 0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, attempts := fakeSSH(t, tc.ssh)
			done := make(chan error, 1)
			go func() { done <- c.Run() }()
			var err error
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("still retrying after %d attempts", attempts())
			}
			if got := exitCode(err); got != tc.code || (tc.code == 0 && err != nil) {
				t.Fatalf("err %v, want exit %d", err, tc.code)
			}
			if n := attempts(); n != tc.attempts {
				t.Fatalf("%d attempts, want %d", n, tc.attempts)
			}
			if retried := strings.Contains(c.Log.(*bytes.Buffer).String(), "reconnecting"); retried != (tc.attempts > 1) {
				t.Fatalf("log %q", c.Log)
			}
		})
	}
}

func TestHosts(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("config", `# comment
Include conf.d/*
Host devbox  devbox.lan
  HostName 10.0.0.5
host=pi
Host *.internal !bad ?x
Host *
  ServerAliveInterval 30
`)
	write("conf.d/orb", "Host orb\nInclude ../config\n")
	write("conf.d/dup", "Host devbox\n")

	got := Hosts(filepath.Join(dir, "config"))
	want := []string{"devbox", "orb", "devbox.lan", "pi"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := Hosts(filepath.Join(dir, "missing")); got != nil {
		t.Fatalf("missing file gave %q", got)
	}
}

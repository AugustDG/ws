package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestQuoteRoundTrips(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	c := &Client{Bin: "tmux", Socket: fmt.Sprintf("ws-quote-test-%d", os.Getpid())}
	t.Cleanup(func() { c.Run("kill-server") })
	if _, err := c.Run("-f", "/dev/null", "new-session", "-d"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`plain`, `it's "quoted"`, `$HOME and \back\slash`, `a;b #{x} ~`} {
		// if-shell parses its command argument as a tmux command string.
		if _, err := c.Run("if-shell", "true", "set-option -g @t "+Quote(s)); err != nil {
			t.Fatal(err)
		}
		got, err := c.Run("show-options", "-gv", "@t")
		if err != nil {
			t.Fatal(err)
		}
		if got != s {
			t.Errorf("Quote(%q) came back as %q", s, got)
		}
	}
}

package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestQuoteRoundTrips runs a quoted run-shell command, the way ws's hooks
// use Quote, and checks what the shell received. Reading an option back
// instead doesn't work: some tmux versions escape $ on output.
func TestQuoteRoundTrips(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	c := &Client{Bin: "tmux", Socket: fmt.Sprintf("ws-quote-test-%d", os.Getpid())}
	t.Cleanup(func() { c.Run("kill-server") })
	if _, err := c.Run("-f", "/dev/null", "new-session", "-d"); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out")
	shQuote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	for _, s := range []string{`plain`, `it's "quoted"`, `$HOME and \back\slash`, `a;b ~ {x}`} {
		script := "printf %s " + shQuote(s) + " > " + shQuote(out)
		// if-shell parses its command argument as a tmux command string.
		if _, err := c.Run("if-shell", "true", "run-shell "+Quote(script)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != s {
			t.Errorf("Quote(%q) came back as %q", s, got)
		}
	}
}

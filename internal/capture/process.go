package capture

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// shells are foreground processes that, run bare, mean "nothing running":
// an idle prompt or a nested shell.
var shells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "tcsh": true, "nu": true,
}

// interpreters run scripts through a shebang, which makes the kernel report
// `node /opt/homebrew/bin/tool args` for a command typed as `tool args`.
var interpreters = map[string]bool{
	"node": true, "python": true, "python3": true, "ruby": true, "perl": true, "bash": true, "sh": true, "bun": true, "deno": true,
}

// Commands says how much of each pane's foreground program capture records.
type Commands int

const (
	NoCommands Commands = iota
	ExecsOnly           // the program name, like `bun`
	WithArgs            // the whole command line, like `bun run bench`
)

// Foreground returns what's running in the foreground of the terminal whose
// first process is panePID, trimmed to mode, or "" when that's an idle shell
// or this ws process's own job (the pane `ws capture` was run from).
// It reads the terminal's foreground process group leader, which for a
// pipeline is the first command. The leader can be panePID itself when the
// pane was started with a program instead of a shell.
//
// ps joins arguments with spaces, so an argument that contained spaces comes
// back unquoted.
func Foreground(panePID int, mode Commands) string {
	if mode == NoCommands {
		return ""
	}
	tpgid, err := ps("tpgid", strconv.Itoa(panePID))
	if err != nil || tpgid == "" || tpgid == "0" || tpgid == "-1" || tpgid == strconv.Itoa(syscall.Getpgrp()) {
		return ""
	}
	args, err := ps("args", tpgid)
	if err != nil {
		return ""
	}
	cmd := Tidy(args, exec.LookPath)
	if mode == ExecsOnly {
		cmd, _, _ = strings.Cut(cmd, " ")
	}
	return cmd
}

func ps(field, pid string) (string, error) {
	out, err := exec.Command("ps", "-o", field+"=", "-p", pid).Output()
	return strings.TrimSpace(string(out)), err
}

// Tidy turns a process's argument list back into what was likely typed:
// absolute paths to commands on PATH become bare names, a shebang
// interpreter in front of such a command is dropped, and a bare shell counts
// as nothing running. lookPath resolves a bare name the way the shell would.
func Tidy(args string, lookPath func(string) (string, error)) string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return ""
	}
	onPath := func(p string) (string, bool) {
		if !filepath.IsAbs(p) {
			return p, true
		}
		name := filepath.Base(p)
		found, err := lookPath(name)
		return name, err == nil && filepath.Clean(found) == filepath.Clean(p)
	}

	if name, ok := onPath(fields[0]); ok {
		fields[0] = name
	}
	if interpreters[fields[0]] && len(fields) > 1 && filepath.IsAbs(fields[1]) {
		if name, ok := onPath(fields[1]); ok {
			fields = append([]string{name}, fields[2:]...)
		}
	}
	if isIdleShell(fields) {
		return ""
	}
	return strings.Join(fields, " ")
}

// isIdleShell reports whether fields is a bare shell like `zsh`, `-zsh` or
// `/bin/zsh -l`, as opposed to a shell running a script.
func isIdleShell(fields []string) bool {
	if !shells[filepath.Base(strings.TrimPrefix(fields[0], "-"))] {
		return false
	}
	for _, f := range fields[1:] {
		if !strings.HasPrefix(f, "-") {
			return false
		}
	}
	return true
}

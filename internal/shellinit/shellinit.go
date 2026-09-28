// Package shellinit renders the shell snippets behind `ws shell-init`. The
// snippet records each command line, as typed, on its tmux pane (the pane
// option named by tmux.TypedCommandOption) just before it runs. `ws capture`
// reads it back, so aliases, `KEY=val cmd` prefixes and quoting survive,
// none of which the running process shows.
package shellinit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AugustDG/ws/internal/tmux"
)

// Shell names a supported shell.
type Shell string

const (
	Zsh  Shell = "zsh"
	Bash Shell = "bash"
)

var scripts = map[Shell]string{
	// preexec's first argument is the line as typed, before alias expansion.
	Zsh: `# ws: record each typed command on its tmux pane for ws capture.
if [[ -n "$TMUX" && -n "$TMUX_PANE" ]]; then
  _ws_record_command() {
    command tmux set-option -p -t "$TMUX_PANE" {{option}} "$1" 2>/dev/null
  }
  autoload -Uz add-zsh-hook
  add-zsh-hook preexec _ws_record_command
fi
`,
	// bash has no preexec. bash-preexec (used by atuin) provides one;
	// otherwise PS0 (bash 4.4+) runs after a line is read and before it
	// runs, and the line is the newest history entry. Older bash gets no
	// hook, and capture falls back to what ps reports.
	Bash: `# ws: record each typed command on its tmux pane for ws capture.
if [[ -n "$TMUX" && -n "$TMUX_PANE" ]]; then
  _ws_record_command() {
    command tmux set-option -p -t "$TMUX_PANE" {{option}} "$1" 2>/dev/null
  }
  if [[ -n "${bash_preexec_imported:-}" ]]; then
    preexec_functions+=(_ws_record_command)
  elif (( BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 4) )); then
    _ws_record_last_history() {
      local line
      line=$(HISTTIMEFORMAT= builtin history 1)
      [[ $line =~ ^[[:space:]]*[0-9]+\*?[[:space:]]+(.*)$ ]] && _ws_record_command "${BASH_REMATCH[1]}"
    }
    PS0="${PS0}"'$(_ws_record_last_history)'
  fi
fi
`,
}

// Script returns the snippet for shell.
func Script(shell Shell) (string, error) {
	s, ok := scripts[shell]
	if !ok {
		return "", fmt.Errorf("unsupported shell %q (supported: %s)", shell, strings.Join(Names(), ", "))
	}
	return strings.ReplaceAll(s, "{{option}}", tmux.TypedCommandOption), nil
}

// Names lists the supported shells.
func Names() []string {
	var out []string
	for s := range scripts {
		out = append(out, string(s))
	}
	sort.Strings(out)
	return out
}

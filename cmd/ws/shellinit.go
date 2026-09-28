package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/shellinit"
)

func shellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init <zsh|bash>",
		Short: "Print the shell hook that lets capture see commands as typed",
		Long: `Print a snippet that records each command line, as typed, on its tmux
pane. ws capture --with-execs / --with-args then replays aliases, KEY=val
prefixes and quoting exactly, instead of what the running process reports.

Add it to your shell's rc file:

  eval "$(ws shell-init zsh)"     # ~/.zshrc
  eval "$(ws shell-init bash)"    # ~/.bashrc (bash 4.4+, or with bash-preexec)`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: shellinit.Names(),
		RunE: func(cmd *cobra.Command, args []string) error {
			script, err := shellinit.Script(shellinit.Shell(args[0]))
			if err != nil {
				return err
			}
			fmt.Print(script)
			return nil
		},
	}
}

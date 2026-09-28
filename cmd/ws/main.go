package main

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/AugustDG/ws/internal/engine"
	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/state"
	"github.com/AugustDG/ws/internal/tmux"
	"github.com/AugustDG/ws/internal/workspace"
)

var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ws",
		Short: "Open tmux workspaces from reusable layouts",
		Long: `ws opens projects as tmux sessions built from YAML layouts.

Run it with no arguments for a picker over running sessions and projects.
Config lives in ~/.config/ws (projects/, layouts/, config.yaml).`,
		Version:      version,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE:         runPicker,
	}
	cmd.AddCommand(
		initCmd(),
		startCmd(),
		stopCmd(),
		lsCmd(),
		captureCmd(),
		importCmd(),
		newCmd(),
		editCmd(),
		checkCmd(),
	)
	return cmd
}

// app is the wiring every command shares.
type app struct {
	cfg  *project.Config
	tmux *tmux.Client
	mgr  *workspace.Manager
}

func newApp() (*app, error) {
	cfg, err := project.Load()
	if err != nil {
		return nil, err
	}
	store, err := state.Default()
	if err != nil {
		return nil, err
	}
	tc := tmux.New()
	w, h := terminalSize(tc)
	return &app{
		cfg:  cfg,
		tmux: tc,
		mgr: &workspace.Manager{
			Config: cfg,
			Tmux:   tc,
			State:  store,
			Engine: &engine.Engine{Tmux: tc, Width: w, Height: h},
			Log:    os.Stderr,
		},
	}, nil
}

// terminalSize is the size a new session should start at: the current tmux
// client, else this terminal, minus a line for the status bar.
func terminalSize(tc *tmux.Client) (int, int) {
	if w, h, ok := tc.ClientSize(); ok {
		return w, max(h-1, 1)
	}
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		return w, max(h-1, 1)
	}
	return 200, 50
}

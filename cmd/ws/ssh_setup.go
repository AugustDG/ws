package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/remote"
	"github.com/AugustDG/ws/internal/tmux"
)

// releaseURL is where the rolling release publishes binaries.
const releaseURL = "https://github.com/AugustDG/ws/releases/download/latest/"

func sshSetupCmd() *cobra.Command {
	var check, force, noConfig bool
	cmd := &cobra.Command{
		Use:   "setup HOST",
		Short: "Install tmux and ws on a host and copy your config there",
		Long: `Prepare HOST for ws ssh:

  tmux         installed with the host's package manager (sudo may prompt)
  ws           this binary, or the release build for the host's platform,
               in ~/.local/bin
  terminfo     entries for your terminal and tmux, if the host lacks them
  tmux config  ~/.config/tmux, plugins included
  ws config    config.yaml and layouts/ (projects stay here: their paths
               are this machine's)

Every step is skipped when the host is already up to date, so it's safe
to rerun after changing your config. Config the host already has, which
setup didn't write, is left alone unless --force is given; then it's kept
as a .ws-bak copy. Setup uses one ssh connection, so it authenticates once.`,
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeHosts,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			return a.setupHost(args[0], check, force, noConfig)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "show what would change and stop")
	cmd.Flags().BoolVar(&force, "force", false, "replace config the host already has (kept as .ws-bak)")
	cmd.Flags().BoolVar(&noConfig, "no-config", false, "don't copy tmux or ws config")
	return cmd
}

func (a *app) setupHost(host string, check, force, noConfig bool) error {
	if err := (&remote.Conn{Host: host}).Validate(); err != nil {
		return err
	}
	controlDir := filepath.Join(a.mgr.State.Dir, "ssh")
	if err := os.MkdirAll(controlDir, 0o700); err != nil {
		return err
	}
	sh := &remote.Shell{Host: host, ControlDir: controlDir}
	defer sh.Close()

	terms := remote.HaveTerminfo(a.localTerms())
	out, err := sh.Output(remote.ProbeScript(terms))
	if err != nil {
		return fmt.Errorf("probing %s: %w", host, err)
	}
	probe, err := remote.ParseProbe(out)
	if err != nil {
		return err
	}

	local := remote.Local{Terms: terms}
	var bin string
	if probe.Supported() {
		var cleanup func()
		if bin, cleanup, err = wsBinary(probe); err != nil {
			return err
		}
		defer cleanup()
		if local.WSHash, err = remote.FileHash(bin); err != nil {
			return err
		}
	}
	tmuxConf := remote.Bundle{Root: filepath.Join(a.cfg.Home, ".config", "tmux")}
	wsConf := remote.Bundle{Root: a.cfg.Dir, Paths: remote.WSConfigPaths}
	if !noConfig {
		if local.TmuxHash, err = tmuxConf.Hash(); err != nil {
			return err
		}
		if local.WSConfHash, err = wsConf.Hash(); err != nil {
			return err
		}
	}

	plan := remote.NewPlan(probe, local, force)
	if noConfig {
		plan.TmuxConf = remote.Step{Name: "tmux config", Note: "skipped (--no-config)"}
		plan.WSConf = remote.Step{Name: "ws config", Note: "skipped (--no-config)"}
	}
	printPlan(host, probe, plan)
	if check {
		return nil
	}

	var errs []error
	run := func(s remote.Step, do func() error) {
		if !s.Do {
			return
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", s.Name, s.Note)
		if err := do(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", s.Name, err))
		}
	}
	run(plan.Tmux, func() error { return sh.Interactive(plan.TmuxInstall) })
	run(plan.WS, func() error {
		f, err := os.Open(bin)
		if err != nil {
			return err
		}
		defer f.Close()
		return sh.Pipe(remote.InstallWSScript, f)
	})
	run(plan.Terminfo, func() error {
		src, err := remote.TerminfoSource(plan.Terms)
		if err != nil {
			return err
		}
		return sh.Pipe(remote.TerminfoScript, bytes.NewReader(src))
	})
	run(plan.TmuxConf, func() error { return sendBundle(sh, tmuxConf, remote.SyncTmuxScript(local.TmuxHash)) })
	run(plan.WSConf, func() error { return sendBundle(sh, wsConf, remote.SyncWSConfigScript(local.WSConfHash)) })

	for _, s := range plan.Steps() {
		if s.Fail {
			errs = append(errs, fmt.Errorf("%s: %s", s.Name, s.Note))
		}
	}
	if len(errs) == 0 {
		fmt.Fprintf(os.Stderr, "%s is ready: ws ssh %s\n", host, host)
	}
	return errors.Join(errs...)
}

func printPlan(host string, p remote.Probe, plan remote.Plan) {
	fmt.Fprintf(os.Stderr, "%s (%s/%s)\n", host, p.OS, p.Arch)
	for _, s := range plan.Steps() {
		mark := " "
		switch {
		case s.Fail:
			mark = "!"
		case s.Do:
			mark = "+"
		}
		fmt.Fprintf(os.Stderr, "  %s %-12s %s\n", mark, s.Name, s.Note)
	}
}

// localTerms are the terminfo entries a session on the host needs: the
// terminal's own, and tmux's.
func (a *app) localTerms() []string {
	outer := os.Getenv("TERM")
	if tmux.Inside() {
		if t, err := a.tmux.Run("display-message", "-p", "#{client_termname}"); err == nil {
			outer = t
		}
	}
	if outer == "tmux-256color" {
		return []string{outer}
	}
	return []string{outer, "tmux-256color"}
}

// wsBinary is the ws to install on the host: this one when the platforms
// match, else the release build, downloaded to a temp file.
func wsBinary(p remote.Probe) (string, func(), error) {
	if p.OS == runtime.GOOS && p.Arch == runtime.GOARCH {
		exe, err := os.Executable()
		return exe, func() {}, err
	}
	fmt.Fprintf(os.Stderr, "downloading %s\n", p.Asset())
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(releaseURL + p.Asset())
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("downloading %s: %s", p.Asset(), resp.Status)
	}
	f, err := os.CreateTemp("", "ws-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.Remove(f.Name()) }
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, nil
}

// sendBundle streams b as a tar into script on the host.
func sendBundle(sh *remote.Shell, b remote.Bundle, script string) error {
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(b.WriteTar(pw)) }()
	err := sh.Pipe(script, pr)
	pr.Close()
	return err
}

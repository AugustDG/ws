package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/capture"
	"github.com/AugustDG/ws/internal/project"
	"github.com/AugustDG/ws/internal/tmuxinator"
)

func importCmd() *cobra.Command {
	var flags saveFlags
	cmd := &cobra.Command{
		Use:   "import <tmuxinator.yml>",
		Short: "Convert a tmuxinator project",
		Long: `Convert a tmuxinator project into a ws project. Raw layout strings
become percentage splits and leading "cd <dir>" commands become pane dirs.

Writes projects/<name>.yaml (the tmuxinator name unless --project is given).
Add --as to put the windows in a reusable layout instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			tp, err := tmuxinator.Parse(data, a.cfg.Home)
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			for _, w := range tp.Warnings {
				fmt.Fprintln(os.Stderr, "warning:", w)
			}
			if flags.projectName == "" {
				flags.projectName = tp.Name
			}
			if flags.projectName == "" {
				flags.projectName = strings.TrimSuffix(filepath.Base(args[0]), filepath.Ext(args[0]))
			}

			res := capture.Templatize(tp.Windows, capture.Options{
				Root: tp.Root, Home: a.cfg.Home, Generalize: !flags.noGeneral,
			})
			return a.save(res, flags, project.Project{OnStart: tp.OnStart, OnStop: tp.OnStop})
		},
	}
	flags.register(cmd)
	return cmd
}

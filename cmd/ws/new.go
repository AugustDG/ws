package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/project"
)

func newCmd() *cobra.Command {
	var root, layoutName string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a project file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			dir, ok := isDir(a.cfg.ExpandHome(root))
			if !ok {
				return fmt.Errorf("%s is not a directory", root)
			}
			p := project.Project{Root: a.cfg.AbbrevHome(dir), Layout: layoutName}
			path := a.cfg.ProjectFile(args[0])
			if err := project.WriteYAML(path, p, false); err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
	cmd.Flags().StringVarP(&root, "root", "r", ".", "project root")
	cmd.Flags().StringVarP(&layoutName, "layout", "l", "", "layout template to use")
	_ = cmd.RegisterFlagCompletionFunc("layout", completeLayouts)
	return cmd
}

func editCmd() *cobra.Command {
	var isLayout bool
	cmd := &cobra.Command{
		Use:               "edit <name>",
		Short:             "Open a project (or with --layout, a layout) in $EDITOR",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeTargets,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			path := a.cfg.ProjectFile(args[0])
			if isLayout {
				path = a.cfg.LayoutFile(args[0])
			} else if p, err := a.cfg.Project(args[0]); err == nil {
				path = p.File // may be the repo's .ws.yaml
			}
			if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) && !isLayout {
				return fmt.Errorf("no project %q; create it with ws new", args[0])
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			c := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return c.Run()
		},
	}
	cmd.Flags().BoolVar(&isLayout, "layout", false, "edit a layout template")
	return cmd
}

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/capture"
	"github.com/AugustDG/ws/internal/layout"
	"github.com/AugustDG/ws/internal/project"
)

// saveFlags are shared by capture and import, which both end in a
// templatized result that can be printed or written out.
type saveFlags struct {
	layoutName  string
	projectName string
	force       bool
	noGeneral   bool
}

func (f *saveFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.layoutName, "as", "", "write a layout template with this name")
	cmd.Flags().StringVar(&f.projectName, "project", "", "write a project with this name")
	cmd.Flags().BoolVar(&f.force, "force", false, "overwrite existing files")
	cmd.Flags().BoolVar(&f.noGeneral, "no-generalize", false, "don't turn repeated worktree panes into for_each")
}

func captureCmd() *cobra.Command {
	var flags saveFlags
	var session string
	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Turn a running session into a layout",
		Long: `Turn a running session into a layout. Sizes become percentages, dirs
under the session root become relative, and columns or rows that repeat
across a repo's worktrees become for_each: worktree.

Prints YAML unless --as or --project says where to write it.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if session == "" {
				if session, err = a.tmux.CurrentSession(); err != nil {
					return fmt.Errorf("pass --session when running outside tmux")
				}
			}
			windows, root, err := capture.FromSession(a.tmux, session)
			if err != nil {
				return err
			}
			res := capture.Templatize(windows, capture.Options{
				Root: root, Home: a.cfg.Home, Generalize: !flags.noGeneral,
			})
			return a.save(res, flags, project.Project{})
		},
	}
	flags.register(cmd)
	cmd.Flags().StringVarP(&session, "session", "s", "", "session to capture (default: current)")
	return cmd
}

// save writes a templatized result as a layout, a project, both, or prints
// it. base carries project fields that don't come from the windows.
func (a *app) save(res capture.Result, f saveFlags, base project.Project) error {
	for _, n := range res.Notes {
		fmt.Fprintln(os.Stderr, n)
	}
	file := layout.File{Windows: res.Windows}

	if f.layoutName == "" && f.projectName == "" {
		data, err := project.EncodeYAML(file)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(data)
		return err
	}

	if f.layoutName != "" {
		path := a.cfg.LayoutFile(f.layoutName)
		if err := project.WriteYAML(path, file, f.force); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	}
	if f.projectName != "" {
		p := base
		p.Root = a.cfg.AbbrevHome(res.Root)
		p.Worktrees = res.Worktrees
		if f.layoutName != "" {
			p.Layout = f.layoutName
		} else {
			p.Windows = res.Windows
		}
		path := a.cfg.ProjectFile(f.projectName)
		if err := project.WriteYAML(path, p, f.force); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	} else if res.Worktrees != nil {
		fmt.Fprintf(os.Stderr, "use it from a project with root: %s and worktrees: {source: %s, count: %d}\n",
			a.cfg.AbbrevHome(res.Root), res.Worktrees.Source, res.Worktrees.Count)
	}
	return nil
}

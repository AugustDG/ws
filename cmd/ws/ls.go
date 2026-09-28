package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/ui"
)

func lsCmd() *cobra.Command {
	var plain, layouts bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List projects and running sessions",
		Long: `List projects and running sessions. STATE is running, external (a
running session ws didn't start, so it has no leased worktrees) or stopped.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp()
			if err != nil {
				return err
			}
			if layouts {
				names, err := a.cfg.Layouts()
				fmt.Println(strings.Join(names, "\n"))
				return err
			}

			items := a.items()
			if plain {
				for _, it := range items {
					fmt.Println(it.Name)
				}
				return nil
			}
			rows := [][]string{{"NAME", "STATE", "KIND", "ROOT", "WORKTREES"}}
			for _, it := range items {
				var wts []string
				leases, err := a.mgr.Leases(it.Name)
				if err != nil {
					wts = []string{"? " + err.Error()}
				}
				for _, l := range leases {
					wts = append(wts, a.cfg.AbbrevHome(l.Path))
				}
				rows = append(rows, []string{
					it.Name,
					ui.StateStyle(it).Render(ui.State(it)),
					it.Kind.String(),
					a.cfg.AbbrevHome(it.Path),
					strings.Join(wts, ", "),
				})
			}
			fmt.Print(table(rows))
			return nil
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print names only, for scripts and fzf")
	cmd.Flags().BoolVar(&layouts, "layouts", false, "list layout templates instead")
	return cmd
}

// table aligns rows into columns. Widths are measured without color codes,
// which text/tabwriter would count as characters.
func table(rows [][]string) string {
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, cell := range r {
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	var b strings.Builder
	for _, r := range rows {
		for i, cell := range r {
			if i == len(r)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(cell + strings.Repeat(" ", widths[i]-lipgloss.Width(cell)+2))
		}
		b.WriteString("\n")
	}
	return b.String()
}

package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func lsCmd() *cobra.Command {
	var plain, layouts bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List projects and running sessions",
		Args:  cobra.NoArgs,
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
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "NAME\tSTATE\tKIND\tROOT\tWORKTREES")
			for _, it := range items {
				state := "stopped"
				if it.Running {
					state = "running"
				}
				var wts []string
				leases, err := a.mgr.Leases(it.Name)
				if err != nil {
					wts = []string{"? " + err.Error()}
				}
				for _, l := range leases {
					wts = append(wts, a.cfg.AbbrevHome(l.Path))
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.Name, state, it.Kind,
					a.cfg.AbbrevHome(it.Path), strings.Join(wts, ", "))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&plain, "plain", false, "print names only, for scripts and fzf")
	cmd.Flags().BoolVar(&layouts, "layouts", false, "list layout templates instead")
	return cmd
}

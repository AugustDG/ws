package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/project"
)

func initCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create the config dir with a starter config and default layout",
		Long: `Create the config dir (~/.config/ws, or $WS_CONFIG_DIR) with a commented
config.yaml, layouts/default.yaml and an empty projects/. Existing files are
left alone unless --force is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := project.Load()
			if err != nil {
				return err
			}
			res, err := cfg.Init(force)
			for _, p := range res.Written {
				fmt.Println("wrote", cfg.AbbrevHome(p))
			}
			for _, p := range res.Skipped {
				fmt.Println("kept ", cfg.AbbrevHome(p), "(exists; --force to overwrite)")
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}

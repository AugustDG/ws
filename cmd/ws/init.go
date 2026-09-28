package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/AugustDG/ws/internal/project"
)

const nextSteps = `
Next:
  ws start                      open this directory with the default layout
  ws capture --project NAME     save the session you're in as projects/NAME.yaml
  ws                            pick a project or session to open

Optional:
  ws completion zsh > "${fpath[1]}/_ws"
      shell completion (bash and fish work too)
  eval "$(ws shell-init zsh)"
      in ~/.zshrc, so capture records commands as typed
  bind f display-popup -E -w 70% -h 60% "ws"
      in tmux.conf, the picker in a popup
`

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
			if err != nil {
				return err
			}
			fmt.Print(nextSteps)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}

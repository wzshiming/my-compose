package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/wzshiming/my-compose/pkg/plan"
)

func (a *app) downCommand(g *globals) *cobra.Command {
	var volumes bool
	var timeout int
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop and remove containers and networks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			project, err := g.load(ctx)
			if err != nil {
				return err
			}
			r, err := a.runner(g, cmd)
			if err != nil {
				return err
			}
			st, err := state(ctx, r, project, plan.StateQueries)
			if err != nil {
				return err
			}
			opts := plan.DownOptions{Volumes: volumes}
			if cmd.Flags().Changed("timeout") {
				t := time.Duration(timeout) * time.Second
				opts.Timeout = &t
			}
			p, w, err := plan.Down(project, st, opts)
			if err != nil {
				return err
			}
			return r.Run(ctx, p, w)
		},
	}
	cmd.Flags().BoolVarP(&volumes, "volumes", "v", false, "Remove named volumes declared in the volumes section and anonymous volumes attached to containers")
	cmd.Flags().IntVarP(&timeout, "timeout", "t", 0, "Shutdown timeout in seconds (default: each service's stop_grace_period)")
	return cmd
}

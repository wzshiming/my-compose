package cli

import (
	"github.com/spf13/cobra"

	"github.com/wzshiming/my-compose/pkg/plan"
)

func (a *app) logsCommand(g *globals) *cobra.Command {
	var opts plan.LogsOptions
	cmd := &cobra.Command{
		Use:   "logs [SERVICE...]",
		Short: "View output from containers",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := g.load(ctx)
			if err != nil {
				return err
			}
			r, err := a.runner(g, cmd)
			if err != nil {
				return err
			}
			st, err := state(ctx, r, project, plan.Ps)
			if err != nil {
				return err
			}
			opts.Services = args
			p, w, err := plan.ServiceLogs(project, st, opts)
			if err != nil {
				return err
			}
			return r.Run(ctx, p, w)
		},
	}
	cmd.Flags().BoolVarP(&opts.Follow, "follow", "f", false, "Follow log output")
	cmd.Flags().StringVar(&opts.Tail, "tail", "all", "Number of lines to show from the end of the logs for each container")
	return cmd
}

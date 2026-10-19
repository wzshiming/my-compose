package cli

import (
	"github.com/spf13/cobra"

	"github.com/wzshiming/my-compose/pkg/plan"
)

func (a *app) upCommand(g *globals) *cobra.Command {
	var build bool
	cmd := &cobra.Command{
		Use:   "up [SERVICE...]",
		Short: "Create and start containers",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			project, err := g.load(ctx)
			if err != nil {
				return err
			}
			if project, err = project.WithServicesEnabled(args...); err != nil {
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
			p, w, err := plan.Up(project, st, r.Engine.Capabilities(), plan.UpOptions{Build: build, Services: args})
			if err != nil {
				return err
			}
			return r.Run(ctx, p, w)
		},
	}
	cmd.Flags().BoolP("detach", "d", false, "Accepted for compatibility; containers always start in the background")
	cmd.Flags().BoolVar(&build, "build", false, "Build images before starting containers")
	return cmd
}

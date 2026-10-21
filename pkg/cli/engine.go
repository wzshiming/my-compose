package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/spf13/cobra"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/plan"
	"github.com/wzshiming/my-compose/pkg/runner"
)

// runner selects the engine and binds a runner to cmd's streams.
func (a *app) runner(g *globals, cmd *cobra.Command) (*runner.Runner, error) {
	explicit := g.engine
	if explicit == "" {
		explicit = os.Getenv("CONTAINER_COMPOSE_ENGINE")
	}
	e, err := engine.Resolve(explicit, a.goos, a.lookPath, a.candidates)
	if err != nil {
		return nil, err
	}
	if !g.dryRun {
		if _, err := a.lookPath(e.Binary()); err != nil {
			return nil, fmt.Errorf("engine %q selected but %q was not found in PATH; install it or select another with --engine or CONTAINER_COMPOSE_ENGINE", e.Name(), e.Binary())
		}
	}
	return &runner.Runner{Engine: e, DryRun: g.dryRun, Strict: g.strict, Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr()}, nil
}

// state runs the List* steps queries yields and parses each by type; a dry run returns the zero State.
func state(ctx context.Context, r *runner.Runner, project *types.Project, queries func(*types.Project) (*plan.Plan, []plan.Warning, error)) (plan.State, error) {
	if r.DryRun {
		return plan.State{}, nil
	}
	p, _, err := queries(project)
	if err != nil {
		return plan.State{}, err
	}
	var containers []plan.ContainerState
	var networks, volumes, images []string
	for _, step := range p.Steps {
		out, err := r.Query(ctx, step)
		if err != nil {
			return plan.State{}, err
		}
		switch step.(type) {
		case plan.ListContainers:
			containers, err = r.Engine.ParseContainers(out)
		case plan.ListNetworks:
			networks, err = r.Engine.ParseNetworks(out)
		case plan.ListVolumes:
			volumes, err = r.Engine.ParseVolumes(out)
		case plan.ListImages:
			images, err = r.Engine.ParseImages(out)
		}
		if err != nil {
			return plan.State{}, err
		}
	}
	return plan.NewState(project, containers, networks, volumes, images), nil
}

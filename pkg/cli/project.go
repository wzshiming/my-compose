package cli

import (
	"context"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
)

// load reads env files twice like docker compose: before config discovery for COMPOSE_FILE and again from the final project directory.
func (g *globals) load(ctx context.Context) (*types.Project, error) {
	opts, err := composecli.NewProjectOptions(g.files,
		composecli.WithWorkingDirectory(g.projectDir),
		composecli.WithOsEnv,
		composecli.WithEnvFiles(g.envFiles...),
		composecli.WithDotEnv,
		composecli.WithConfigFileEnv,
		composecli.WithDefaultConfigPath,
		composecli.WithEnvFiles(g.envFiles...),
		composecli.WithDotEnv,
		composecli.WithDefaultProfiles(g.profiles...),
		composecli.WithName(g.projectName),
		composecli.WithDiscardEnvFile,
	)
	if err != nil {
		return nil, err
	}
	return opts.LoadProject(ctx)
}

package cli

import "github.com/spf13/cobra"

func configCommand(g *globals) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Parse, resolve and render the compose file in canonical format",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			project, err := g.load(cmd.Context())
			if err != nil {
				return err
			}
			out, err := project.MarshalYAML()
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(out)
			return err
		},
	}
}

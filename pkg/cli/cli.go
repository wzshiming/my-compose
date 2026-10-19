package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/applecontainer"
	"github.com/wzshiming/my-compose/pkg/engine/dockerlike"
)

type app struct {
	candidates []engine.Engine
	lookPath   func(string) (string, error)
	goos       string
}

// globals holds the root command's flags of one run.
type globals struct {
	files       []string
	projectName string
	profiles    []string
	envFiles    []string
	projectDir  string
	engine      string
	dryRun      bool
	strict      bool
}

// Main runs the command line with the real engines and returns the process exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return (&app{candidates: candidates(), lookPath: exec.LookPath, goos: runtime.GOOS}).run(ctx, args, stdout, stderr)
}

func candidates() []engine.Engine {
	var cs []engine.Engine
	for _, v := range dockerlike.Variants() {
		e, _ := dockerlike.New(v) // cannot fail for a name from Variants
		cs = append(cs, e)
	}
	return append(cs, applecontainer.New())
}

func (a *app) run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	g := &globals{}
	started := false
	root := &cobra.Command{
		Use:   "my-compose [flags] COMMAND",
		Short: "Run Compose projects on Apple container, wslc, docker, podman or nerdctl",
		Long: "Run Compose projects on Apple container, wslc, docker, podman or nerdctl.\n\n" +
			"Global flags such as -f, -p, --engine and --dry-run go before the subcommand: my-compose -f compose.yaml --dry-run up",
		SilenceUsage:      true,
		SilenceErrors:     true,
		TraverseChildren:  true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		PersistentPreRun:  func(*cobra.Command, []string) { started = true },
		// Traverse skips cobra's legacyArgs; NoArgs yields the same "unknown command" error.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	fs := root.Flags()
	fs.StringArrayVarP(&g.files, "file", "f", nil, "Compose configuration files")
	fs.StringVarP(&g.projectName, "project-name", "p", "", "Project name")
	fs.StringArrayVar(&g.profiles, "profile", nil, "Specify a profile to enable")
	fs.StringArrayVar(&g.envFiles, "env-file", nil, "Specify an alternate environment file")
	fs.StringVar(&g.projectDir, "project-directory", "", "Specify an alternate working directory (default: the directory of the first specified Compose file)")
	fs.StringVar(&g.engine, "engine", "", "Container engine to use (default: CONTAINER_COMPOSE_ENGINE, else the first engine found in PATH, probed in the order for this OS)")
	fs.BoolVar(&g.dryRun, "dry-run", false, "Print the engine commands instead of running them")
	fs.BoolVar(&g.strict, "strict", false, "Fail instead of warning when the compose file uses attributes the engine cannot express")
	root.AddCommand(a.upCommand(g), a.downCommand(g), a.psCommand(g), a.logsCommand(g), configCommand(g))

	root.SetOut(stdout)
	root.SetErr(stderr)
	if args == nil {
		args = []string{} // cobra reads os.Args[1:] for nil
	}
	root.SetArgs(args)
	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		return 0
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	if !started {
		fmt.Fprint(stderr, cmd.UsageString())
	}
	return exitCode(err)
}

func exitCode(err error) int {
	var exit *exec.ExitError
	switch {
	case errors.Is(err, context.Canceled):
		return 130
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		return exit.ExitCode()
	}
	return 1
}

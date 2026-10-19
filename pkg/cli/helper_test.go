package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// tstHelperEnv marks this test binary re-executed as the engine command of a live run.
const tstHelperEnv = "GO_TEST_HELPER_PROCESS"

func TestMain(m *testing.M) {
	if os.Getenv(tstHelperEnv) == "1" {
		os.Exit(tstHelperMain(os.Args))
	}
	os.Exit(m.Run())
}

// tstHelperMain is the child process; after "--" it takes "print <text...>" (text to stdout), "exit <code> [stdout] [stderr]" or "sleep" (idles until killed).
func tstHelperMain(argv []string) int {
	i := slices.Index(argv, "--")
	if i < 0 || i+1 >= len(argv) {
		fmt.Fprintln(os.Stderr, "tst helper: missing mode after --")
		return 2
	}
	args := argv[i+1:]
	switch args[0] {
	case "print":
		os.Stdout.WriteString(strings.Join(args[1:], " "))
		return 0
	case "exit":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "tst helper: usage: exit <code> [stdout] [stderr]")
			return 2
		}
		code, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "tst helper:", err)
			return 2
		}
		if len(args) > 2 {
			os.Stdout.WriteString(args[2])
		}
		if len(args) > 3 {
			os.Stderr.WriteString(args[3])
		}
		return code
	case "sleep":
		time.Sleep(30 * time.Second)
		return 0
	}
	fmt.Fprintf(os.Stderr, "tst helper: unknown mode %q\n", args[0])
	return 2
}

// tstArgv is the engine argv that makes the helper process run mode with args.
func tstArgv(mode string, args ...string) []string {
	return append([]string{"--", mode}, args...)
}

// tstEngine is an enginetest.Fake whose command lines re-run this test binary as the helper process: the List queries print an empty JSON array and every other step runs the argv scripted in On for its step type, "exit 0" by default.
type tstEngine struct {
	*enginetest.Fake
	On   map[string][]string // helper argv by step type name, such as "RunContainer"
	Warn []plan.Warning      // returned by every Render other than the List queries
}

func tstFake(name string) *tstEngine {
	e := &tstEngine{Fake: enginetest.NewFake(name), On: map[string][]string{}}
	e.BinaryV = os.Args[0]
	e.RenderFn = func(step plan.Step) ([]string, []plan.Warning, error) {
		switch step.(type) {
		case plan.ListContainers, plan.ListNetworks, plan.ListVolumes, plan.ListImages:
			return tstArgv("print", "[]"), nil, nil
		}
		if argv, ok := e.On[reflect.TypeOf(step).Name()]; ok {
			return argv, e.Warn, nil
		}
		return tstArgv("exit", "0"), e.Warn, nil
	}
	return e
}

// tstApp builds the command line over fakes on a linux where every binary is found.
func tstApp(fakes ...*tstEngine) *app {
	candidates := make([]engine.Engine, 0, len(fakes))
	for _, f := range fakes {
		candidates = append(candidates, f)
	}
	return &app{
		candidates: candidates,
		lookPath:   func(string) (string, error) { return "", nil },
		goos:       "linux",
	}
}

// tstIsolateEnv hides the compose and engine variables of the surrounding shell for the test's duration.
func tstIsolateEnv(t *testing.T) {
	t.Helper()
	tstUnsetenv(t, "COMPOSE_FILE", "COMPOSE_PATH_SEPARATOR", "COMPOSE_PROJECT_NAME", "COMPOSE_PROFILES", "COMPOSE_DISABLE_ENV_FILE", "CONTAINER_COMPOSE_ENGINE")
}

// tstUnsetenv unsets keys, restoring them when the test ends; unlike t.Setenv(k, "") an unset COMPOSE_FILE is really ignored.
func tstUnsetenv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { os.Setenv(k, v) })
			os.Unsetenv(k)
		}
	}
}

func tstWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tstProject writes yaml as compose.yaml in a fresh temporary directory and returns that directory.
func tstProject(t *testing.T, yaml string) string {
	t.Helper()
	dir := t.TempDir()
	tstWrite(t, filepath.Join(dir, "compose.yaml"), yaml)
	return dir
}

// tstArgs selects the fake engine and project "p" from dir's compose.yaml ahead of args.
func tstArgs(dir string, args ...string) []string {
	return append([]string{"--engine", "fake", "-p", "p", "-f", filepath.Join(dir, "compose.yaml")}, args...)
}

func tstRun(t *testing.T, a *app, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return tstRunCtx(context.Background(), t, a, args...)
}

// tstRunCtx runs a with child processes entering the helper mode of TestMain.
func tstRunCtx(ctx context.Context, t *testing.T, a *app, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Setenv(tstHelperEnv, "1")
	var out, errb bytes.Buffer
	code = a.run(ctx, args, &out, &errb)
	t.Logf("run %s\n  exit %d\n  stdout: %q\n  stderr: %q", strings.Join(args, " "), code, out.String(), errb.String())
	return code, out.String(), errb.String()
}

func tstLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// tstKinds lists the step type names, such as "ListContainers", in order.
func tstKinds(steps []plan.Step) []string {
	kinds := make([]string, len(steps))
	for i, s := range steps {
		kinds[i] = reflect.TypeOf(s).Name()
	}
	return kinds
}

func tstStepsOf[T plan.Step](steps []plan.Step) []T {
	var out []T
	for _, s := range steps {
		if v, ok := s.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func tstRunNames(steps []plan.Step) []string {
	var names []string
	for _, r := range tstStepsOf[plan.RunContainer](steps) {
		names = append(names, r.Container.Name)
	}
	return names
}

// tstContainer is a container of project with the compose labels NewState keys on.
func tstContainer(project, service string, n int, status, health string) plan.ContainerState {
	return plan.ContainerState{
		Name:    plan.ContainerName(project, service, n),
		Service: service,
		Number:  n,
		Status:  status,
		Health:  health,
		Labels: map[string]string{
			plan.LabelProject:         project,
			plan.LabelService:         service,
			plan.LabelContainerNumber: strconv.Itoa(n),
		},
	}
}

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/sirupsen/logrus"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

const tstWebYAML = "services:\n  web:\n    image: nginx:1.27\n"

func TestHelp(t *testing.T) {
	tstIsolateEnv(t)
	for _, args := range [][]string{{}, {"--help"}} {
		code, out, errb := tstRun(t, tstApp(), args...)
		if code != 0 || errb != "" {
			t.Fatalf("%q: exit %d, stderr %q; want 0 and nothing on stderr", args, code, errb)
		}
		for _, sub := range []string{"up", "down", "ps", "logs", "config"} {
			if !regexp.MustCompile(`(?m)^  ` + sub + `\b`).MatchString(out) {
				t.Errorf("%q: help on stdout lists no %q subcommand:\n%s", args, sub, out)
			}
		}
		if strings.Contains(out, "completion") {
			t.Errorf("%q: help mentions the completion command:\n%s", args, out)
		}
	}
}

func TestUsageErrors(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, tstWebYAML)

	t.Run("errors before execution append the usage", func(t *testing.T) {
		for _, tt := range []struct {
			args  []string
			token string
		}{
			{[]string{"--bogus"}, "bogus"},
			{[]string{"down", "extra"}, "extra"},
			{[]string{"nosuch"}, "nosuch"},
			{[]string{"down", "-t", "abc"}, "abc"},
		} {
			code, _, errb := tstRun(t, tstApp(tstFake("fake")), tt.args...)
			lines := tstLines(errb)
			if code != 1 || len(lines) == 0 || !strings.HasPrefix(lines[0], "error: ") || !strings.Contains(lines[0], tt.token) {
				t.Errorf("%q: exit %d, stderr %q; want 1 and a first line \"error: ...%s...\"", tt.args, code, errb, tt.token)
				continue
			}
			if !strings.Contains(errb, "Usage:") {
				t.Errorf("%q: stderr lacks the usage after the error:\n%s", tt.args, errb)
			}
			if n := tstCountPrefixed(lines, "error: "); n != 1 {
				t.Errorf("%q: %d error lines, want exactly one:\n%s", tt.args, n, errb)
			}
		}
	})

	t.Run("run time errors stand alone", func(t *testing.T) {
		for _, tt := range []struct {
			args []string
			msg  string
		}{
			{tstArgs(dir, "ps", "--format", "x"), `error: unsupported format "x": use table or json`},
			{[]string{"--engine", "bogus", "-p", "p", "-f", filepath.Join(dir, "compose.yaml"), "up"}, `error: unknown engine "bogus"`},
		} {
			code, _, errb := tstRun(t, tstApp(tstFake("fake")), tt.args...)
			lines := tstLines(errb)
			if code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], tt.msg) {
				t.Errorf("%q: exit %d, stderr %q; want 1 and the single line %q", tt.args, code, errb, tt.msg)
			}
			if strings.Contains(errb, "Usage:") {
				t.Errorf("%q: a run time error must not print the usage:\n%s", tt.args, errb)
			}
		}
	})
}

func tstCountPrefixed(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func TestEngineSelection(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, tstWebYAML)
	file := filepath.Join(dir, "compose.yaml")
	notFound := errors.New("executable file not found in $PATH")

	t.Run("explicit engine is used without probing PATH", func(t *testing.T) {
		fake := tstFake("fake")
		var looked []string
		a := &app{
			candidates: []engine.Engine{fake},
			lookPath:   func(f string) (string, error) { looked = append(looked, f); return "", nil },
			goos:       "linux",
		}
		if code, _, errb := tstRun(t, a, tstArgs(dir, "--dry-run", "up")...); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, errb)
		}
		if len(looked) != 0 {
			t.Errorf("lookPath called with %q, want no call for an explicit --engine", looked)
		}
		if len(fake.Rendered) == 0 {
			t.Error("the explicit engine rendered nothing")
		}
	})

	t.Run("explicit unknown engine", func(t *testing.T) {
		code, _, errb := tstRun(t, tstApp(tstFake("fake")), "--engine", "bogus", "-p", "p", "-f", file, "--dry-run", "up")
		if code != 1 || !strings.Contains(errb, `unknown engine "bogus"`) {
			t.Errorf("exit %d, stderr %q; want 1 and the unknown engine error", code, errb)
		}
	})

	t.Run("CONTAINER_COMPOSE_ENGINE selects when --engine is empty", func(t *testing.T) {
		t.Setenv("CONTAINER_COMPOSE_ENGINE", "fake")
		fake := tstFake("fake")
		if code, _, errb := tstRun(t, tstApp(fake), "-p", "p", "-f", file, "--dry-run", "up"); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, errb)
		}
		if len(fake.Rendered) == 0 {
			t.Error("the engine named by CONTAINER_COMPOSE_ENGINE rendered nothing")
		}
	})

	t.Run("--engine wins over CONTAINER_COMPOSE_ENGINE", func(t *testing.T) {
		t.Setenv("CONTAINER_COMPOSE_ENGINE", "bogus")
		if code, _, errb := tstRun(t, tstApp(tstFake("fake")), tstArgs(dir, "--dry-run", "up")...); code != 0 {
			t.Errorf("exit %d, stderr %q; want 0", code, errb)
		}
	})

	t.Run("probing takes the first candidate found in PATH", func(t *testing.T) {
		docker, podman := enginetest.NewFake("docker"), enginetest.NewFake("podman")
		a := &app{
			candidates: []engine.Engine{docker, podman},
			lookPath: func(f string) (string, error) {
				if f == "podman" {
					return "/usr/bin/podman", nil
				}
				return "", notFound
			},
			goos: "linux",
		}
		if code, _, errb := tstRun(t, a, "-p", "p", "-f", file, "--dry-run", "up"); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, errb)
		}
		if len(podman.Rendered) == 0 || len(docker.Rendered) != 0 {
			t.Errorf("rendered %d steps with podman and %d with docker, want podman only", len(podman.Rendered), len(docker.Rendered))
		}
	})

	t.Run("probing finds nothing", func(t *testing.T) {
		a := &app{
			candidates: []engine.Engine{enginetest.NewFake("docker"), enginetest.NewFake("podman")},
			lookPath:   func(string) (string, error) { return "", notFound },
			goos:       "linux",
		}
		code, _, errb := tstRun(t, a, "-p", "p", "-f", file, "--dry-run", "up")
		if code != 1 || !strings.Contains(errb, "no container engine found") || strings.Contains(errb, "Usage:") {
			t.Errorf("exit %d, stderr %q; want 1 and the no engine error without usage", code, errb)
		}
	})

	t.Run("live run needs the binary, dry run does not", func(t *testing.T) {
		fake := tstFake("fake")
		var looked []string
		a := &app{
			candidates: []engine.Engine{fake},
			lookPath:   func(f string) (string, error) { looked = append(looked, f); return "", notFound },
			goos:       "linux",
		}
		code, _, errb := tstRun(t, a, tstArgs(dir, "up")...)
		if code != 1 || !strings.Contains(errb, "selected but") || !strings.Contains(errb, "was not found in PATH") {
			t.Errorf("live: exit %d, stderr %q; want 1 and the missing binary error", code, errb)
		}
		if !slices.Contains(looked, fake.Binary()) {
			t.Errorf("lookPath called with %q, want the engine binary %q", looked, fake.Binary())
		}
		if code, _, errb := tstRun(t, a, tstArgs(dir, "--dry-run", "up")...); code != 0 {
			t.Errorf("dry run: exit %d, stderr %q; want 0", code, errb)
		}
	})
}

const tstUpLiveYAML = `services:
  web:
    image: nginx:1.27
    ports:
      - "8080:80"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost"]
      interval: 5s
  db:
    image: postgres:16
`

func TestUpLive(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, tstUpLiveYAML)
	newFake := func() *tstEngine {
		f := tstFake("fake")
		f.Containers = []plan.ContainerState{tstContainer("p", "web", 1, plan.StatusRunning, plan.HealthHealthy)}
		return f
	}

	t.Run("queries the state and runs only the missing containers", func(t *testing.T) {
		f := newFake()
		if code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, "up")...); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, errb)
		}
		kinds := tstKinds(f.Rendered)
		queries := []string{"ListContainers", "ListNetworks", "ListVolumes", "ListImages"}
		if len(kinds) < 4 || !reflect.DeepEqual(kinds[:4], queries) {
			t.Errorf("rendered %q, want the queries %q first", kinds, queries)
		}
		if got := f.ParsedInputs["ParseContainers"]; len(got) != 1 || strings.TrimSpace(string(got[0])) != "[]" {
			t.Errorf("ParseContainers inputs = %q, want the single query output []", got)
		}
		if names := tstRunNames(f.Rendered); !reflect.DeepEqual(names, []string{"p-db-1"}) {
			t.Errorf("RunContainer steps for %q, want only p-db-1 since p-web-1 is running", names)
		}
	})

	t.Run("--build rebuilds an existing image", func(t *testing.T) {
		bdir := tstProject(t, "services:\n  app:\n    build: .\n")
		for _, tt := range []struct {
			args  []string
			built bool
		}{
			{tstArgs(bdir, "up"), false},
			{tstArgs(bdir, "up", "--build"), true},
		} {
			f := tstFake("fake")
			f.Images = []string{"p-app"}
			if code, _, errb := tstRun(t, tstApp(f), tt.args...); code != 0 {
				t.Fatalf("%q: exit %d, stderr %q", tt.args, code, errb)
			}
			if built := len(tstStepsOf[plan.BuildImage](f.Rendered)) > 0; built != tt.built {
				t.Errorf("%q: BuildImage rendered = %v, want %v (image p-app exists)", tt.args, built, tt.built)
			}
		}
	})

	t.Run("exit code of a failing step", func(t *testing.T) {
		f := newFake()
		f.On["RunContainer"] = tstArgv("exit", "3")
		code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, "up")...)
		lines := tstLines(errb)
		if code != 3 || len(lines) != 1 || !strings.HasPrefix(lines[0], "error: ") || !strings.Contains(lines[0], "exit status 3") {
			t.Errorf("exit %d, stderr %q; want 3 and a single error line naming exit status 3", code, errb)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		f := newFake()
		f.On["RunContainer"] = tstArgv("sleep")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(time.Second, cancel)
		start := time.Now()
		code, _, errb := tstRunCtx(ctx, t, tstApp(f), tstArgs(dir, "up")...)
		if code != 130 || errb != "error: context canceled\n" {
			t.Errorf("exit %d, stderr %q after %v; want 130 and \"error: context canceled\"", code, errb, time.Since(start))
		}
	})

	t.Run("--strict stops before the first process", func(t *testing.T) {
		warn := plan.Warning{Scope: "services.db", Attribute: "tst", Message: "ignored (tst)"}
		f := newFake()
		f.Warn = []plan.Warning{warn}
		f.On["RunContainer"] = tstArgv("print", "RAN")
		code, out, errb := tstRun(t, tstApp(f), tstArgs(dir, "up")...)
		if code != 0 || !strings.Contains(errb, "warning: "+warn.String()+"\n") || !strings.Contains(out, "RAN") {
			t.Fatalf("control run: exit %d, stdout %q, stderr %q; want 0, the step output and the warning", code, out, errb)
		}

		f = newFake()
		f.Warn = []plan.Warning{warn}
		f.On["RunContainer"] = tstArgv("print", "RAN")
		code, out, errb = tstRun(t, tstApp(f), tstArgs(dir, "--strict", "up")...)
		if code != 1 || !strings.Contains(errb, "warning: "+warn.String()+"\n") || !strings.Contains(errb, "error: strict:") {
			t.Errorf("strict run: exit %d, stderr %q; want 1 with the warning and a strict error", code, errb)
		}
		if !slices.Contains(tstKinds(f.Rendered), "RunContainer") {
			t.Errorf("strict run rendered %q, want the RunContainer step rendered before refusing", tstKinds(f.Rendered))
		}
		if strings.Contains(out, "RAN") {
			t.Errorf("strict run executed a step: stdout %q", out)
		}
	})
}

func TestUpServices(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, "services:\n  web:\n    image: nginx:1.27\n  debugsvc:\n    image: alpine:3.20\n    profiles: [debug]\n")
	for _, tt := range []struct {
		args []string
		runs []string
	}{
		{[]string{"up"}, []string{"p-web-1"}},
		{[]string{"up", "-d"}, []string{"p-web-1"}},
		{[]string{"up", "debugsvc"}, []string{"p-debugsvc-1"}},
	} {
		f := tstFake("fake")
		if code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, append([]string{"--dry-run"}, tt.args...)...)...); code != 0 {
			t.Fatalf("%q: exit %d, stderr %q", tt.args, code, errb)
		}
		if names := tstRunNames(f.Rendered); !reflect.DeepEqual(names, tt.runs) {
			t.Errorf("%q: RunContainer steps for %q, want %q", tt.args, names, tt.runs)
		}
	}
	code, _, errb := tstRun(t, tstApp(tstFake("fake")), tstArgs(dir, "--dry-run", "up", "nosuch")...)
	if code != 1 || errb != "error: no such service: nosuch\n" {
		t.Errorf("up nosuch: exit %d, stderr %q; want 1 and the no such service error", code, errb)
	}
}

func TestDown(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, "services:\n  web:\n    image: nginx:1.27\n    volumes:\n      - data:/data\nvolumes:\n  data: {}\n")
	three := 3 * time.Second
	for _, tt := range []struct {
		args    []string
		timeout *time.Duration
		volumes bool
	}{
		{[]string{"down"}, nil, false},
		{[]string{"down", "-t", "3"}, &three, false},
		{[]string{"down", "-v"}, nil, true},
	} {
		f := tstFake("fake")
		if code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, append([]string{"--dry-run"}, tt.args...)...)...); code != 0 {
			t.Fatalf("%q: exit %d, stderr %q", tt.args, code, errb)
		}
		stops := tstStepsOf[plan.StopContainers](f.Rendered)
		if len(stops) != 1 || !reflect.DeepEqual(stops[0].Timeout, tt.timeout) {
			t.Errorf("%q: StopContainers = %+v, want one with Timeout %v", tt.args, stops, tt.timeout)
		}
		removes := tstStepsOf[plan.RemoveContainers](f.Rendered)
		if len(removes) != 1 || removes[0].Volumes != tt.volumes {
			t.Errorf("%q: RemoveContainers = %+v, want one with Volumes %v", tt.args, removes, tt.volumes)
		}
		if vols := tstStepsOf[plan.RemoveVolume](f.Rendered); (len(vols) > 0) != tt.volumes {
			t.Errorf("%q: RemoveVolume steps %+v, want present = %v", tt.args, vols, tt.volumes)
		}
	}
}

func TestLogs(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, tstWebYAML)

	t.Run("live without containers", func(t *testing.T) {
		f := tstFake("fake")
		code, out, errb := tstRun(t, tstApp(f), tstArgs(dir, "logs")...)
		if code != 0 || out != "" || errb != "" {
			t.Errorf("exit %d, stdout %q, stderr %q; want 0 and no output", code, out, errb)
		}
		if kinds := tstKinds(f.Rendered); !reflect.DeepEqual(kinds, []string{"ListContainers"}) {
			t.Errorf("rendered %q, want only the ListContainers query", kinds)
		}
	})

	t.Run("unknown service", func(t *testing.T) {
		code, _, errb := tstRun(t, tstApp(tstFake("fake")), tstArgs(dir, "logs", "nosuch")...)
		if code != 1 || errb != "error: no such service: nosuch\n" {
			t.Errorf("exit %d, stderr %q; want 1 and the no such service error", code, errb)
		}
	})

	t.Run("flags", func(t *testing.T) {
		for _, tt := range []struct {
			args []string
			want []plan.Logs
		}{
			{[]string{"logs"}, []plan.Logs{{Name: "p-web-1", Follow: false, Tail: "all"}}},
			{[]string{"logs", "-f", "--tail", "10", "web"}, []plan.Logs{{Name: "p-web-1", Follow: true, Tail: "10"}}},
		} {
			f := tstFake("fake")
			if code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, append([]string{"--dry-run"}, tt.args...)...)...); code != 0 {
				t.Fatalf("%q: exit %d, stderr %q", tt.args, code, errb)
			}
			if got := tstStepsOf[plan.Logs](f.Rendered); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("%q: Logs steps %+v, want %+v", tt.args, got, tt.want)
			}
		}
	})
}

func TestConfig(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, "services:\n  web:\n    image: nginx:1.27\n    ports:\n      - \"8080:80\"\n    volumes:\n      - ./html:/usr/share/nginx/html:ro\n")
	file := filepath.Join(dir, "compose.yaml")
	opts, err := composecli.NewProjectOptions([]string{file}, composecli.WithOsEnv, composecli.WithName("p"))
	if err != nil {
		t.Fatal(err)
	}
	project, err := opts.LoadProject(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantYAML, err := project.MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	want := string(wantYAML)
	if !strings.Contains(want, "name: p\n") {
		t.Fatalf("MarshalYAML lacks the project name:\n%s", want)
	}

	code, out, errb := tstRun(t, tstApp(), "-p", "p", "-f", file, "config")
	if code != 0 || errb != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errb)
	}
	if out != want {
		t.Errorf("config printed:\n%s\nwant MarshalYAML:\n%s", out, want)
	}

	noEngine := &app{
		candidates: nil,
		lookPath:   func(string) (string, error) { return "", errors.New("not found") },
		goos:       "linux",
	}
	code, out, errb = tstRun(t, noEngine, "--engine", "bogus", "--dry-run", "--strict", "-p", "p", "-f", file, "config")
	if code != 0 || out != want {
		t.Errorf("without engines and with engine flags: exit %d, stderr %q, stdout differs = %v; want 0 and the same yaml", code, errb, out != want)
	}
}

func TestLogrusWarnings(t *testing.T) {
	tstIsolateEnv(t)
	tstUnsetenv(t, "TST_UNSET")
	dir := tstProject(t, "services:\n  web:\n    image: nginx:1.27\n    environment:\n      FOO: \"${TST_UNSET}\"\n")
	const want = `warning: The "TST_UNSET" variable is not set. Defaulting to a blank string.`
	for _, args := range [][]string{
		tstArgs(dir, "--dry-run", "up"),
		tstArgs(dir, "--strict", "--dry-run", "up"),
	} {
		code, _, errb := tstRun(t, tstApp(tstFake("fake")), args...)
		if code != 0 {
			t.Errorf("%q: exit %d, stderr %q; want 0", args, code, errb)
		}
		if !slices.Contains(tstLines(errb), want) {
			t.Errorf("%q: stderr %q lacks the line %q", args, errb, want)
		}
		if strings.Contains(errb, "level=") || strings.Contains(errb, "WARN[") {
			t.Errorf("%q: stderr %q uses the default logrus format", args, errb)
		}
		std := logrus.StandardLogger()
		if _, text := std.Formatter.(*logrus.TextFormatter); std.Out != os.Stderr || !text || std.GetLevel() != logrus.InfoLevel {
			t.Errorf("%q: logrus after the run has Out %T, Formatter %T, Level %v; want the defaults os.Stderr, *logrus.TextFormatter and info restored", args, std.Out, std.Formatter, std.GetLevel())
		}
	}
}

func TestMultiFileProfileName(t *testing.T) {
	tstIsolateEnv(t)
	dir := filepath.Join(t.TempDir(), "dirname")
	tstWrite(t, filepath.Join(dir, "a.yaml"), "services:\n  web:\n    image: nginx:1\n  extra:\n    image: alpine:3.20\n    profiles: [x]\n")
	tstWrite(t, filepath.Join(dir, "b.yaml"), "services:\n  web:\n    image: nginx:2\n")
	files := []string{"--dry-run", "--engine", "fake", "-f", filepath.Join(dir, "a.yaml"), "-f", filepath.Join(dir, "b.yaml")}

	run := func(t *testing.T, extra ...string) []plan.RunContainer {
		t.Helper()
		f := tstFake("fake")
		args := append(append([]string{}, files...), extra...)
		if code, _, errb := tstRun(t, tstApp(f), append(args, "up")...); code != 0 {
			t.Fatalf("%q: exit %d, stderr %q", args, code, errb)
		}
		return tstStepsOf[plan.RunContainer](f.Rendered)
	}
	names := func(runs []plan.RunContainer) []string {
		var out []string
		for _, r := range runs {
			out = append(out, r.Container.Name)
		}
		return out
	}

	runs := run(t)
	if got := names(runs); !reflect.DeepEqual(got, []string{"dirname-web-1"}) {
		t.Errorf("merged files: RunContainer %q, want dirname-web-1 only (project named after the directory)", got)
	}
	if len(runs) > 0 && runs[0].Container.Image != "nginx:2" {
		t.Errorf("merged files: web image %q, want nginx:2 from the second file", runs[0].Container.Image)
	}
	if got := names(run(t, "--profile", "x")); !reflect.DeepEqual(got, []string{"dirname-extra-1", "dirname-web-1"}) {
		t.Errorf("--profile x: RunContainer %q, want extra and web", got)
	}
	if got := names(run(t, "-p", "custom")); !reflect.DeepEqual(got, []string{"custom-web-1"}) {
		t.Errorf("-p custom: RunContainer %q, want custom-web-1", got)
	}
}

func TestEnvFile(t *testing.T) {
	tstIsolateEnv(t)
	tstUnsetenv(t, "TAG")
	const yaml = "services:\n  web:\n    image: \"nginx:${TAG}\"\n"
	dir := tstProject(t, yaml)
	tstWrite(t, filepath.Join(dir, ".env"), "TAG=7\n")
	tstWrite(t, filepath.Join(dir, "custom.env"), "TAG=9\n")
	other := tstProject(t, yaml)
	for _, tt := range []struct {
		name  string
		args  []string
		image string
	}{
		{"default .env", tstArgs(dir, "--dry-run", "up"), "nginx:7"},
		{"--env-file", tstArgs(dir, "--env-file", filepath.Join(dir, "custom.env"), "--dry-run", "up"), "nginx:9"},
		{"--project-directory locates .env", tstArgs(other, "--project-directory", dir, "--dry-run", "up"), "nginx:7"},
	} {
		f := tstFake("fake")
		if code, _, errb := tstRun(t, tstApp(f), tt.args...); code != 0 {
			t.Fatalf("%s: exit %d, stderr %q", tt.name, code, errb)
		}
		runs := tstStepsOf[plan.RunContainer](f.Rendered)
		if len(runs) != 1 || runs[0].Container.Image != tt.image {
			t.Errorf("%s: RunContainer %+v, want one with image %q", tt.name, runs, tt.image)
		}
	}
}

func TestMainDryRun(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, tstWebYAML)
	var out, errb strings.Builder
	code := Main(context.Background(), []string{"--dry-run", "--engine", "docker", "-p", "demo", "-f", filepath.Join(dir, "compose.yaml"), "up"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	if lines := tstLines(out.String()); len(lines) == 0 || !strings.HasPrefix(lines[0], "docker ") {
		t.Errorf("stdout %q, want docker command lines", out.String())
	}
}

func TestMainNilArgs(t *testing.T) {
	tstIsolateEnv(t)
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = []string{saved[0], "bogus"}
	var out, errb strings.Builder
	code := Main(context.Background(), nil, &out, &errb)
	if code != 0 || errb.String() != "" || !strings.Contains(out.String(), "Usage:") {
		t.Errorf("Main(nil args): exit %d, stderr %q, stdout %q; want 0 and the help on stdout, never the process arguments", code, errb.String(), out.String())
	}
}

func TestSignalExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal death is a Unix concept")
	}
	tstIsolateEnv(t)
	dir := tstProject(t, tstWebYAML)
	f := tstFake("fake")
	f.BinaryV = "sh"
	f.RenderFn = func(step plan.Step) ([]string, []plan.Warning, error) {
		switch step.(type) {
		case plan.ListContainers, plan.ListNetworks, plan.ListVolumes, plan.ListImages:
			return []string{"-c", "echo []"}, nil, nil
		case plan.RunContainer:
			return []string{"-c", "kill -9 $$"}, nil, nil
		}
		return []string{"-c", "exit 0"}, nil, nil
	}
	code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, "up")...)
	lines := tstLines(errb)
	if code != 1 || len(lines) != 1 || !strings.HasPrefix(lines[0], "error: ") || !strings.Contains(lines[0], "signal: killed") {
		t.Errorf("exit %d, stderr %q; want 1 and a single error line naming the signal, since a killed engine has no exit status", code, errb)
	}
}

func TestDownLiveVolumes(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, "services:\n  web:\n    image: nginx:1.27\n    volumes:\n      - data:/data\nvolumes:\n  data: {}\n")
	f := tstFake("fake")
	f.Networks = []string{"p_default"}
	f.Volumes = []string{"p_data"}
	if code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, "down", "-v")...); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb)
	}
	var nets, vols []string
	for _, s := range tstStepsOf[plan.RemoveNetwork](f.Rendered) {
		nets = append(nets, s.Name)
	}
	for _, s := range tstStepsOf[plan.RemoveVolume](f.Rendered) {
		vols = append(vols, s.Name)
	}
	if !reflect.DeepEqual(nets, []string{"p_default"}) || !reflect.DeepEqual(vols, []string{"p_data"}) {
		t.Errorf("RemoveNetwork %q, RemoveVolume %q; want [p_default] and [p_data] from the ListNetworks and ListVolumes queries", nets, vols)
	}
}

func TestComposeProfilesEnv(t *testing.T) {
	tstIsolateEnv(t)
	t.Setenv("COMPOSE_PROFILES", "debug")
	dir := tstProject(t, "services:\n  web:\n    image: nginx:1.27\n  debugsvc:\n    image: alpine:3.20\n    profiles: [debug]\n")
	f := tstFake("fake")
	if code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, "--dry-run", "up")...); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb)
	}
	if names := tstRunNames(f.Rendered); !reflect.DeepEqual(names, []string{"p-debugsvc-1", "p-web-1"}) {
		t.Errorf("RunContainer %q, want debugsvc and web with COMPOSE_PROFILES=debug", names)
	}
}

func TestOsEnvWinsOverDotEnv(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, "services:\n  web:\n    image: \"nginx:${TAG}\"\n")
	tstWrite(t, filepath.Join(dir, ".env"), "TAG=fromdotenv\n")
	t.Setenv("TAG", "fromos")
	f := tstFake("fake")
	if code, _, errb := tstRun(t, tstApp(f), tstArgs(dir, "--dry-run", "up")...); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb)
	}
	if runs := tstStepsOf[plan.RunContainer](f.Rendered); len(runs) != 1 || runs[0].Container.Image != "nginx:fromos" {
		t.Errorf("RunContainer %+v, want one with image nginx:fromos (the OS environment over .env)", runs)
	}
}

func TestComposeFileEnvDotEnv(t *testing.T) {
	tstIsolateEnv(t)
	tstUnsetenv(t, "TAG")
	dir := tstProject(t, "services:\n  web:\n    image: \"nginx:${TAG}\"\n")
	tstWrite(t, filepath.Join(dir, ".env"), "TAG=7\n")
	t.Setenv("COMPOSE_FILE", filepath.Join(dir, "compose.yaml"))
	f := tstFake("fake")
	if code, _, errb := tstRun(t, tstApp(f), "--engine", "fake", "-p", "p", "--dry-run", "up"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb)
	}
	if runs := tstStepsOf[plan.RunContainer](f.Rendered); len(runs) != 1 || runs[0].Container.Image != "nginx:7" {
		t.Errorf("RunContainer %+v, want one with image nginx:7 from the .env beside COMPOSE_FILE", runs)
	}
}

//go:build e2e

// Package e2e runs the stack scenario through a real engine and compares its normalized transcript with the golden every engine shares.
package e2e

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/distribution/reference"

	"github.com/wzshiming/my-compose/pkg/cli"
	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/container"
	"github.com/wzshiming/my-compose/pkg/engine/docker"
	"github.com/wzshiming/my-compose/pkg/engine/nerdctl"
	"github.com/wzshiming/my-compose/pkg/engine/podman"
	"github.com/wzshiming/my-compose/pkg/engine/wslc"
	"github.com/wzshiming/my-compose/pkg/plan"
	"github.com/wzshiming/my-compose/pkg/runner"
)

var update = flag.Bool("update", false, "rewrite transcript.golden from this run")

const project = "e2e"

// stack drives my-compose and the engine's own list queries against testdata/stack.
type stack struct {
	t      *testing.T
	dir    string
	runner *runner.Runner
}

func TestStack(t *testing.T) {
	name := os.Getenv("MY_COMPOSE_E2E_ENGINE")
	var e engine.Engine
	switch name {
	case "":
		t.Fatal("MY_COMPOSE_E2E_ENGINE is not set: choose docker, podman, nerdctl, wslc or container")
	case "docker":
		e = docker.New()
	case "podman":
		e = podman.New()
	case "nerdctl":
		e = nerdctl.New()
	case "wslc":
		e = wslc.New()
	case "container":
		e = container.New()
	default:
		t.Fatalf("MY_COMPOSE_E2E_ENGINE=%q is not one of docker, podman, nerdctl, wslc or container", name)
	}
	if _, err := exec.LookPath(e.Binary()); err != nil {
		t.Fatalf("MY_COMPOSE_E2E_ENGINE=%s: %v", name, err)
	}
	for _, k := range []string{"COMPOSE_FILE", "COMPOSE_PATH_SEPARATOR", "COMPOSE_PROJECT_NAME", "COMPOSE_PROFILES", "COMPOSE_DISABLE_ENV_FILE", "CONTAINER_COMPOSE_ENGINE"} {
		os.Unsetenv(k)
	}
	dir, err := filepath.Abs(filepath.Join("testdata", "stack"))
	if err != nil {
		t.Fatal(err)
	}
	var tr []string
	record := os.Getenv("MY_COMPOSE_E2E_RECORD")
	if record != "" {
		if err := os.MkdirAll(record, 0o755); err != nil {
			t.Fatal(err)
		}
		// Written from Cleanup so a Fatal mid-run still leaves the partial transcript.
		t.Cleanup(func() { write(t, filepath.Join(record, "transcript.txt"), []byte(strings.Join(tr, "\n")+"\n")) })
	}
	s := &stack{t: t, dir: dir, runner: &runner.Runner{Engine: e, Stderr: os.Stderr}}
	s.run("down", "-v") // leftovers of an interrupted run
	t.Cleanup(func() { s.run("down", "-v") })

	code, _, _ := s.run("up")
	tr = append(tr, "## up", fmt.Sprintf("up: exit %d", code))
	if record != "" {
		for file, step := range map[string]plan.Step{
			"ps.json":              plan.ListContainers{Project: project},
			"inspect_running.json": plan.InspectContainer{Name: "e2e-api-1"},
			"networks.json":        plan.ListNetworks{},
			"volumes.json":         plan.ListVolumes{},
			"images.json":          plan.ListImages{},
		} {
			write(t, filepath.Join(record, file), s.query(step))
		}
	}
	tr = append(tr, s.has("network", plan.ListNetworks{}, "e2e_backend"), s.has("volume", plan.ListVolumes{}, "e2e_data"), s.has("image", plan.ListImages{}, "e2e-app:latest"))

	code, lines := s.ps()
	tr = append(tr, "## ps", fmt.Sprintf("ps: exit %d", code))
	tr = append(tr, lines...)
	code, lines = s.ps("-a")
	tr = append(tr, "## ps -a", fmt.Sprintf("ps -a: exit %d", code))
	tr = append(tr, lines...)
	code, out, _ := s.run("ps", "-q")
	names := strings.Fields(out)
	slices.Sort(names)
	tr = append(tr, "## ps -q", fmt.Sprintf("ps -q: exit %d", code))
	tr = append(tr, names...)

	code, out, _ = s.run("logs")
	var logs []string
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		if prefix, msg, ok := strings.Cut(line, " | "); ok {
			line = strings.TrimRight(prefix, " ") + " | " + msg
		}
		if line != "" {
			logs = append(logs, line)
		}
	}
	slices.Sort(logs)
	tr = append(tr, "## logs", fmt.Sprintf("logs: exit %d", code))
	tr = append(tr, logs...)

	code, out, errOut := s.run("up")
	tr = append(tr, "## up again", fmt.Sprintf("up again: exit %d stdout-empty=%t stderr-empty=%t", code, out == "", errOut == ""))

	code, _, _ = s.run("down")
	tr = append(tr, "## down", fmt.Sprintf("down: exit %d", code))
	code, left := s.ps("-a")
	tr = append(tr, fmt.Sprintf("ps -a: exit %d", code))
	if len(left) == 0 {
		tr = append(tr, "ps -a: none")
	} else {
		tr = append(tr, left...)
	}
	tr = append(tr, s.has("network", plan.ListNetworks{}, "e2e_backend"), s.has("volume", plan.ListVolumes{}, "e2e_data"), s.has("image", plan.ListImages{}, "e2e-app:latest"))

	code, _, _ = s.run("down", "-v")
	tr = append(tr, "## down -v", fmt.Sprintf("down -v: exit %d", code), s.has("volume", plan.ListVolumes{}, "e2e_data"))

	got := strings.Join(tr, "\n") + "\n"
	golden := filepath.Join(dir, "transcript.golden")
	if *update {
		write(t, golden, []byte(got))
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		w, g := append(strings.Split(string(want), "\n"), "<end>"), append(strings.Split(got, "\n"), "<end>")
		i := 0
		for w[i] == g[i] {
			i++
		}
		t.Errorf("transcript differs from %s at line %d:\n  golden: %q\n  output: %q\nrun go test -tags e2e ./test/e2e -args -update after reviewing", golden, i+1, w[i], g[i])
	}
}

// run executes one my-compose command against the stack with a fresh timeout and logs its command line, exit code and streams.
func (s *stack) run(args ...string) (code int, stdout, stderr string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	args = append([]string{"--engine", s.runner.Engine.Name(), "--strict", "-p", project, "--project-directory", s.dir, "-f", filepath.Join(s.dir, "compose.yaml")}, args...)
	var out, errb bytes.Buffer
	code = cli.Main(ctx, args, &out, &errb)
	s.t.Logf("my-compose %s\nexit %d\nstdout:\n%sstderr:\n%s", strings.Join(args, " "), code, out.String(), errb.String())
	return code, out.String(), errb.String()
}

// query runs one engine list or inspect command with a fresh timeout and returns its raw stdout.
func (s *stack) query(step plan.Step) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := s.runner.Query(ctx, step)
	if err != nil {
		s.t.Fatalf("%T: %v", step, err)
	}
	return out
}

// has reports whether the engine lists name among the kind step enumerates, images compared by image.
func (s *stack) has(kind string, step plan.Step, name string) string {
	out := s.query(step)
	var names []string
	var err error
	switch step.(type) {
	case plan.ListNetworks:
		names, err = s.runner.Engine.ParseNetworks(out)
	case plan.ListVolumes:
		names, err = s.runner.Engine.ParseVolumes(out)
	case plan.ListImages:
		names, err = s.runner.Engine.ParseImages(out)
		for i, n := range names {
			names[i] = image(n)
		}
	}
	if err != nil {
		s.t.Fatalf("%T: %v", step, err)
	}
	state := "absent"
	if slices.Contains(names, name) {
		state = "present"
	}
	return fmt.Sprintf("%s %s: %s", kind, name, state)
}

// ps runs ps --format json with args and returns its exit code and one line per container sorted by name: name service number image status health exit_code ports.
func (s *stack) ps(args ...string) (int, []string) {
	code, out, _ := s.run(append([]string{"ps", "--format", "json"}, args...)...)
	if code != 0 {
		return code, nil
	}
	var entries []struct {
		Name, Service, Image, Status, Health string
		Number                               int
		ExitCode                             int `json:"exit_code"`
		Ports                                []struct {
			HostIP              string `json:"host_ip"`
			Published, Protocol string
			Target              int
		}
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		s.t.Fatalf("ps %s: %v", strings.Join(args, " "), err)
	}
	lines := make([]string, 0, len(entries))
	for _, c := range entries {
		var ports []string
		for _, p := range c.Ports {
			port := fmt.Sprintf("%d/%s", p.Target, p.Protocol)
			if p.Published != "" {
				port = p.Published + "->" + port
			}
			if p.HostIP != "" {
				port = p.HostIP + ":" + port
			}
			ports = append(ports, port)
		}
		lines = append(lines, fmt.Sprintf("%s %s %d %s %s %s %d %s", c.Name, c.Service, c.Number, image(c.Image), c.Status, c.Health, c.ExitCode, cmp.Or(strings.Join(ports, ","), "-")))
	}
	slices.Sort(lines)
	return code, lines
}

// image reduces equivalent references to name:tag, so docker.io/library/busybox:1.36 and localhost/e2e-app:latest read busybox:1.36 and e2e-app:latest.
func image(s string) string {
	named, err := reference.ParseDockerRef(s)
	if err != nil {
		return s
	}
	name := strings.TrimPrefix(reference.Path(named), "library/")
	if tagged, ok := named.(reference.Tagged); ok {
		return name + ":" + tagged.Tag()
	}
	return name
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

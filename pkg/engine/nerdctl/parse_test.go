package nerdctl_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/nerdctl"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// testdata holds nerdctl's list and inspect output, printed as JSON lines, for the same three
// containers as the other CLIs' samples: p-web-1 running and healthy, p-worker-2 exited 1, p-init-1 created.

// What the samples encode in nerdctl's own spelling.
const (
	tstNginx   = "docker.io/library/nginx:1.27"   // image of p-web-1
	tstBusybox = "docker.io/library/busybox:1.36" // image of p-worker-2 and p-init-1
)

var (
	tstCommand  = []string{"nginx", "-g", "'daemon", "off;'"} // p-web-1 Command after ps parsing: a string split on whitespace
	tstNetworks = []string{"p_default", "bridge", "host", "none"}
	tstVolumes  = []string{"p_data"}
	tstImages   = []string{"docker.io/library/nginx:1.27", "docker.io/library/p-web:latest"}
)

func tstRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tstEqual[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

func TestParseContainers(t *testing.T) {
	got, err := nerdctl.New().ParseContainers(tstRead(t, "ps.json"))
	if err != nil {
		t.Fatalf("ParseContainers error: %v", err)
	}
	var names []string
	for _, c := range got {
		names = append(names, c.Name)
	}
	if want := []string{"p-web-1", "p-worker-2", "p-init-1"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %q, want %q", names, want)
	}
	web, worker, created := got[0], got[1], got[2]

	tstEqual(t, "web.Service", web.Service, "web")
	tstEqual(t, "web.Number", web.Number, 1)
	tstEqual(t, "web.Image", web.Image, tstNginx)
	tstEqual(t, "web.Status", web.Status, plan.StatusRunning)
	tstEqual(t, "web.Health", web.Health, plan.HealthHealthy)
	tstEqual(t, "web.ExitCode", web.ExitCode, 0)
	tstEqual(t, "web.Labels[config_files]", web.Labels[plan.LabelConfigFiles], "/a/compose.yaml,/a/override.yaml")
	tstEqual(t, "web.Labels[project]", web.Labels[plan.LabelProject], "p")
	tstEqual(t, "web.Labels[working_dir]", web.Labels[plan.LabelWorkingDir], "/a")
	tstEqual(t, "web.Labels[oneoff]", web.Labels[plan.LabelOneoff], "False")
	if len(web.Ports) == 0 {
		t.Errorf("web.Ports is empty")
	} else {
		tstEqual(t, "web.Ports[0]", web.Ports[0], plan.Port{HostIP: "0.0.0.0", Published: "8080", Target: 80, Protocol: "tcp"})
	}
	tstEqual(t, "web.Command", web.Command, tstCommand)

	tstEqual(t, "worker.Service", worker.Service, "worker")
	tstEqual(t, "worker.Number", worker.Number, 2)
	tstEqual(t, "worker.Image", worker.Image, tstBusybox)
	tstEqual(t, "worker.Status", worker.Status, plan.StatusExited)
	tstEqual(t, "worker.Health", worker.Health, plan.HealthNone)
	tstEqual(t, "worker.ExitCode", worker.ExitCode, 1)
	tstEqual(t, "len(worker.Ports)", len(worker.Ports), 0)

	tstEqual(t, "created.Service", created.Service, "init")
	tstEqual(t, "created.Number", created.Number, 1)
	tstEqual(t, "created.Status", created.Status, plan.StatusCreated)
	tstEqual(t, "created.Health", created.Health, plan.HealthNone)
	tstEqual(t, "created.ExitCode", created.ExitCode, 0)
	tstEqual(t, "created.Command", created.Command, []string{"sleep", "1"})
}

func TestParseInspect(t *testing.T) {
	e := nerdctl.New()

	web, err := e.ParseInspect(tstRead(t, "inspect_running.json"))
	if err != nil {
		t.Fatalf("ParseInspect(running) error: %v", err)
	}
	tstEqual(t, "web.Name", web.Name, "p-web-1")
	tstEqual(t, "web.Image", web.Image, tstNginx)
	tstEqual(t, "web.Status", web.Status, plan.StatusRunning)
	tstEqual(t, "web.Health", web.Health, plan.HealthHealthy)
	tstEqual(t, "web.ExitCode", web.ExitCode, 0)
	tstEqual(t, "web.Labels[service]", web.Labels[plan.LabelService], "web")
	tstEqual(t, "web.Labels[config_files]", web.Labels[plan.LabelConfigFiles], "/a/compose.yaml,/a/override.yaml")
	tstEqual(t, "web.Command", web.Command, []string{"nginx", "-g", "daemon off;"})
	tstEqual(t, "len(web.Ports)", len(web.Ports), 0)

	worker, err := e.ParseInspect(tstRead(t, "inspect_exited.json"))
	if err != nil {
		t.Fatalf("ParseInspect(exited) error: %v", err)
	}
	tstEqual(t, "worker.Name", worker.Name, "p-worker-2")
	tstEqual(t, "worker.Image", worker.Image, tstBusybox)
	tstEqual(t, "worker.Status", worker.Status, plan.StatusExited)
	tstEqual(t, "worker.Health", worker.Health, plan.HealthNone)
	tstEqual(t, "worker.ExitCode", worker.ExitCode, 1)
	tstEqual(t, "worker.Labels[service]", worker.Labels[plan.LabelService], "worker")
	tstEqual(t, "worker.Command", worker.Command, []string{"sh", "-c", "exit 1"})

	created, err := e.ParseInspect(tstRead(t, "inspect_created.json"))
	if err != nil {
		t.Fatalf("ParseInspect(created) error: %v", err)
	}
	tstEqual(t, "created.Name", created.Name, "p-init-1")
	tstEqual(t, "created.Image", created.Image, tstBusybox)
	tstEqual(t, "created.Status", created.Status, plan.StatusCreated)
	tstEqual(t, "created.Health", created.Health, plan.HealthNone)
	tstEqual(t, "created.ExitCode", created.ExitCode, 0)
	tstEqual(t, "created.Command", created.Command, []string{"sleep", "1"})
}

func TestParseNames(t *testing.T) {
	e := nerdctl.New()
	networks, err := e.ParseNetworks(tstRead(t, "networks.json"))
	if err != nil {
		t.Errorf("ParseNetworks error: %v", err)
	}
	tstEqual(t, "networks", networks, tstNetworks)
	volumes, err := e.ParseVolumes(tstRead(t, "volumes.json"))
	if err != nil {
		t.Errorf("ParseVolumes error: %v", err)
	}
	tstEqual(t, "volumes", volumes, tstVolumes)
	images, err := e.ParseImages(tstRead(t, "images.json"))
	if err != nil {
		t.Errorf("ParseImages error: %v", err)
	}
	tstEqual(t, "images", images, tstImages)
}

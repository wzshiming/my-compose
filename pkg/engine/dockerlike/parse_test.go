package dockerlike_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wzshiming/my-compose/pkg/plan"
)

// testdata/<variant> holds each CLI's list and inspect output for the same three containers
// (p-web-1 running and healthy, p-worker-2 exited 1, p-init-1 created): docker and nerdctl
// print JSON lines, podman prints arrays. The wslc samples are constructed from the docker
// shapes as arrays with State and HealthStatus fields and await re-recording on Windows.
// The podman inspect samples also cover two older shapes: State.Healthcheck instead of
// State.Health (running) and a missing Config.Image falling back to ImageName (created).

// tstSample is what a variant's samples encode in the CLI's own spelling.
type tstSample struct {
	nginx    string   // image of p-web-1
	busybox  string   // image of p-worker-2 and p-init-1
	command  []string // p-web-1 Command after ps parsing: a string split on whitespace, or the array as is
	networks []string
	volumes  []string
	images   []string
}

var tstSamples = map[string]tstSample{
	"docker": {
		nginx: "nginx:1.27", busybox: "busybox:1.36",
		command:  []string{"nginx", "-g", "'daemon", "off;'"},
		networks: []string{"p_default", "bridge", "host", "none"},
		volumes:  []string{"p_data", "3f0e9c2b7a1d4e6f8b0a2c4e6f8a0b2c4d6e8f0a1b3c5d7e9f1a3b5c7d9e1f3a"},
		images:   []string{"nginx:1.27", "p-web:latest"},
	},
	"podman": {
		nginx: "docker.io/library/nginx:1.27", busybox: "docker.io/library/busybox:1.36",
		command:  []string{"nginx", "-g", "daemon off;"},
		networks: []string{"p_default", "podman"},
		volumes:  []string{"p_data", "3f0e9c2b7a1d4e6f8b0a2c4e6f8a0b2c4d6e8f0a1b3c5d7e9f1a3b5c7d9e1f3a"},
		images:   []string{"docker.io/library/nginx:1.27", "docker.io/library/nginx:latest", "localhost/p-web:latest"},
	},
	"nerdctl": {
		nginx: "docker.io/library/nginx:1.27", busybox: "docker.io/library/busybox:1.36",
		command:  []string{"nginx", "-g", "'daemon", "off;'"},
		networks: []string{"p_default", "bridge", "host", "none"},
		volumes:  []string{"p_data"},
		images:   []string{"docker.io/library/nginx:1.27", "docker.io/library/p-web:latest"},
	},
	"wslc": {
		nginx: "nginx:1.27", busybox: "busybox:1.36",
		command:  []string{"nginx", "-g", "'daemon", "off;'"},
		networks: []string{"p_default", "bridge", "host", "none"},
		volumes:  []string{"p_data"},
		images:   []string{"nginx:1.27", "p-web:latest"},
	},
}

func tstRead(t *testing.T, variant, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", variant, name))
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
	for _, v := range tstVariants {
		t.Run(v, func(t *testing.T) {
			sample := tstSamples[v]
			got, err := tstNew(t, v).ParseContainers(tstRead(t, v, "ps.json"))
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
			tstEqual(t, "web.Image", web.Image, sample.nginx)
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
			tstEqual(t, "web.Command", web.Command, sample.command)

			tstEqual(t, "worker.Service", worker.Service, "worker")
			tstEqual(t, "worker.Number", worker.Number, 2)
			tstEqual(t, "worker.Image", worker.Image, sample.busybox)
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
		})
	}
}

// tstFields is the subset of ContainerState the single-object cases below pin.
type tstFields struct {
	Name, Status, Health string
	ExitCode             int
	Service              string
	Number               int
}

func TestParseContainersFields(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want tstFields
	}{
		{"names string drops leading slash", `{"Names":"/x"}`, tstFields{Name: "x", Status: plan.StatusUnknown, Health: plan.HealthNone}},
		{"names string keeps first of comma list", `{"Names":"x,y"}`, tstFields{Name: "x", Status: plan.StatusUnknown, Health: plan.HealthNone}},
		{"names array keeps first", `{"Names":["/x","y"]}`, tstFields{Name: "x", Status: plan.StatusUnknown, Health: plan.HealthNone}},
		{"state field wins over status", `{"Names":"x","State":"paused","Status":"Up 1 minute"}`, tstFields{Name: "x", Status: plan.StatusPaused, Health: plan.HealthNone}},
		{"state is lowercased", `{"Names":"x","State":"Running"}`, tstFields{Name: "x", Status: plan.StatusRunning, Health: plan.HealthNone}},
		{"status up", `{"Names":"x","Status":"Up 1 minute"}`, tstFields{Name: "x", Status: plan.StatusRunning, Health: plan.HealthNone}},
		{"status up paused", `{"Names":"x","Status":"Up 1 minute (Paused)"}`, tstFields{Name: "x", Status: plan.StatusPaused, Health: plan.HealthNone}},
		{"status exited with code", `{"Names":"x","Status":"Exited (137) 2 hours ago"}`, tstFields{Name: "x", Status: plan.StatusExited, Health: plan.HealthNone, ExitCode: 137}},
		{"status exited without code", `{"Names":"x","State":"exited","Status":"Exited"}`, tstFields{Name: "x", Status: plan.StatusExited, Health: plan.HealthNone, ExitCode: -1}},
		{"status created", `{"Names":"x","Status":"Created"}`, tstFields{Name: "x", Status: plan.StatusCreated, Health: plan.HealthNone}},
		{"status restarting", `{"Names":"x","Status":"Restarting (1) 2 seconds ago"}`, tstFields{Name: "x", Status: plan.StatusRestarting, Health: plan.HealthNone}},
		{"status dead", `{"Names":"x","Status":"Dead","ExitCode":0}`, tstFields{Name: "x", Status: plan.StatusDead, Health: plan.HealthNone}},
		{"status unknown word", `{"Names":"x","Status":"Removal In Progress"}`, tstFields{Name: "x", Status: plan.StatusUnknown, Health: plan.HealthNone}},
		{"podman stopped", `{"Names":["x"],"State":"stopped","Status":"Exited (0) 3 seconds ago","ExitCode":0}`, tstFields{Name: "x", Status: plan.StatusExited, Health: plan.HealthNone}},
		{"podman configured", `{"Names":["x"],"State":"configured","Status":"Created","ExitCode":0}`, tstFields{Name: "x", Status: plan.StatusCreated, Health: plan.HealthNone}},
		{"podman initialized", `{"Names":["x"],"State":"initialized","Status":"Created","ExitCode":0}`, tstFields{Name: "x", Status: plan.StatusCreated, Health: plan.HealthNone}},
		{"unknown state", `{"Names":"x","State":"weird","Status":"Up 1 minute"}`, tstFields{Name: "x", Status: plan.StatusUnknown, Health: plan.HealthNone}},
		{"health healthy from status", `{"Names":"x","State":"running","Status":"Up 2 minutes (healthy)"}`, tstFields{Name: "x", Status: plan.StatusRunning, Health: plan.HealthHealthy}},
		{"health unhealthy from status", `{"Names":"x","State":"running","Status":"Up 2 minutes (unhealthy)"}`, tstFields{Name: "x", Status: plan.StatusRunning, Health: plan.HealthUnhealthy}},
		{"health starting from status", `{"Names":"x","State":"running","Status":"Up 2 seconds (health: starting)"}`, tstFields{Name: "x", Status: plan.StatusRunning, Health: plan.HealthStarting}},
		{"health status field wins", `{"Names":"x","State":"running","Status":"Up 1 minute (unhealthy)","HealthStatus":"healthy"}`, tstFields{Name: "x", Status: plan.StatusRunning, Health: plan.HealthHealthy}},
		{"exit code field wins", `{"Names":"x","State":"exited","Status":"Exited (1) 3 seconds ago","ExitCode":3}`, tstFields{Name: "x", Status: plan.StatusExited, Health: plan.HealthNone, ExitCode: 3}},
		{"exit code is zero while running", `{"Names":"x","State":"running","Status":"Up 1 minute"}`, tstFields{Name: "x", Status: plan.StatusRunning, Health: plan.HealthNone}},
		{"service and number from label map", `{"Names":"x","Labels":{"com.docker.compose.service":"web","com.docker.compose.container-number":"3"}}`, tstFields{Name: "x", Status: plan.StatusUnknown, Health: plan.HealthNone, Service: "web", Number: 3}},
		{"service and number from label string", `{"Names":"x","Labels":"com.docker.compose.container-number=3,com.docker.compose.service=web"}`, tstFields{Name: "x", Status: plan.StatusUnknown, Health: plan.HealthNone, Service: "web", Number: 3}},
	}
	for _, v := range tstVariants {
		e := tstNew(t, v)
		for _, tt := range tests {
			t.Run(v+"/"+tt.name, func(t *testing.T) {
				got, err := e.ParseContainers([]byte(tt.in))
				if err != nil || len(got) != 1 {
					t.Fatalf("ParseContainers(%s) = %d containers, %v; want 1, nil", tt.in, len(got), err)
				}
				c := got[0]
				tstEqual(t, "fields", tstFields{Name: c.Name, Status: c.Status, Health: c.Health, ExitCode: c.ExitCode, Service: c.Service, Number: c.Number}, tt.want)
			})
		}
	}
}

func TestParseContainersLabels(t *testing.T) {
	e := tstNew(t, "docker")
	t.Run("string with comma inside a value", func(t *testing.T) {
		got, err := e.ParseContainers([]byte(`{"Names":"x","Labels":"a=1,b=x,y,c=3"}`))
		if err != nil || len(got) != 1 {
			t.Fatalf("ParseContainers = %v, %v; want one container", got, err)
		}
		tstEqual(t, "Labels", got[0].Labels, map[string]string{"a": "1", "b": "x,y", "c": "3"})
	})
	t.Run("missing labels give an empty map", func(t *testing.T) {
		got, err := e.ParseContainers([]byte(`{"Names":"x"}`))
		if err != nil || len(got) != 1 {
			t.Fatalf("ParseContainers = %v, %v; want one container", got, err)
		}
		if got[0].Labels == nil || len(got[0].Labels) != 0 {
			t.Errorf("Labels = %#v, want an empty non-nil map", got[0].Labels)
		}
	})
	t.Run("empty label string gives an empty map", func(t *testing.T) {
		got, err := e.ParseContainers([]byte(`{"Names":"x","Labels":""}`))
		if err != nil || len(got) != 1 {
			t.Fatalf("ParseContainers = %v, %v; want one container", got, err)
		}
		if got[0].Labels == nil || len(got[0].Labels) != 0 {
			t.Errorf("Labels = %#v, want an empty non-nil map", got[0].Labels)
		}
	})
}

func TestParseContainersPorts(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []plan.Port
	}{
		{
			"docker string",
			`{"Names":"x","Ports":"0.0.0.0:8080->80/tcp, :::8080->80/tcp, [::]:8443->443/tcp, 443/tcp, 0.0.0.0:9000-9010->9000-9010/udp, garbage"}`,
			[]plan.Port{
				{HostIP: "0.0.0.0", Published: "8080", Target: 80, Protocol: "tcp"},
				{HostIP: "::", Published: "8080", Target: 80, Protocol: "tcp"},
				{HostIP: "::", Published: "8443", Target: 443, Protocol: "tcp"},
				{Target: 443, Protocol: "tcp"},
				{HostIP: "0.0.0.0", Published: "9000-9010", Target: 9000, Protocol: "udp"},
			},
		},
		{
			"podman array",
			`[{"Names":["x"],"Ports":[{"host_ip":"","container_port":80,"host_port":8080,"range":1,"protocol":"tcp"},{"host_ip":"::1","container_port":443,"host_port":8443,"range":1,"protocol":"tcp"}]}]`,
			[]plan.Port{
				{Published: "8080", Target: 80, Protocol: "tcp"},
				{HostIP: "::1", Published: "8443", Target: 443, Protocol: "tcp"},
			},
		},
		{"empty string", `{"Names":"x","Ports":""}`, nil},
		{"null array", `[{"Names":["x"],"Ports":null}]`, nil},
	}
	for _, v := range tstVariants {
		e := tstNew(t, v)
		for _, tt := range tests {
			t.Run(v+"/"+tt.name, func(t *testing.T) {
				got, err := e.ParseContainers([]byte(tt.in))
				if err != nil || len(got) != 1 {
					t.Fatalf("ParseContainers = %d containers, %v; want 1, nil", len(got), err)
				}
				if len(tt.want) == 0 {
					tstEqual(t, "len(Ports)", len(got[0].Ports), 0)
					return
				}
				tstEqual(t, "Ports", got[0].Ports, tt.want)
			})
		}
	}
}

func TestParseInspect(t *testing.T) {
	for _, v := range tstVariants {
		t.Run(v, func(t *testing.T) {
			e := tstNew(t, v)
			sample := tstSamples[v]

			web, err := e.ParseInspect(tstRead(t, v, "inspect_running.json"))
			if err != nil {
				t.Fatalf("ParseInspect(running) error: %v", err)
			}
			tstEqual(t, "web.Name", web.Name, "p-web-1")
			tstEqual(t, "web.Image", web.Image, sample.nginx)
			tstEqual(t, "web.Status", web.Status, plan.StatusRunning)
			tstEqual(t, "web.Health", web.Health, plan.HealthHealthy)
			tstEqual(t, "web.ExitCode", web.ExitCode, 0)
			tstEqual(t, "web.Labels[service]", web.Labels[plan.LabelService], "web")
			tstEqual(t, "web.Labels[config_files]", web.Labels[plan.LabelConfigFiles], "/a/compose.yaml,/a/override.yaml")
			tstEqual(t, "web.Command", web.Command, []string{"nginx", "-g", "daemon off;"})
			tstEqual(t, "len(web.Ports)", len(web.Ports), 0)

			worker, err := e.ParseInspect(tstRead(t, v, "inspect_exited.json"))
			if err != nil {
				t.Fatalf("ParseInspect(exited) error: %v", err)
			}
			tstEqual(t, "worker.Name", worker.Name, "p-worker-2")
			tstEqual(t, "worker.Image", worker.Image, sample.busybox)
			tstEqual(t, "worker.Status", worker.Status, plan.StatusExited)
			tstEqual(t, "worker.Health", worker.Health, plan.HealthNone)
			tstEqual(t, "worker.ExitCode", worker.ExitCode, 1)
			tstEqual(t, "worker.Labels[service]", worker.Labels[plan.LabelService], "worker")
			tstEqual(t, "worker.Command", worker.Command, []string{"sh", "-c", "exit 1"})

			created, err := e.ParseInspect(tstRead(t, v, "inspect_created.json"))
			if err != nil {
				t.Fatalf("ParseInspect(created) error: %v", err)
			}
			tstEqual(t, "created.Name", created.Name, "p-init-1")
			tstEqual(t, "created.Image", created.Image, sample.busybox)
			tstEqual(t, "created.Status", created.Status, plan.StatusCreated)
			tstEqual(t, "created.Health", created.Health, plan.HealthNone)
			tstEqual(t, "created.ExitCode", created.ExitCode, 0)
			tstEqual(t, "created.Command", created.Command, []string{"sleep", "1"})
		})
	}
}

func TestParseNames(t *testing.T) {
	for _, v := range tstVariants {
		t.Run(v, func(t *testing.T) {
			e := tstNew(t, v)
			sample := tstSamples[v]
			networks, err := e.ParseNetworks(tstRead(t, v, "networks.json"))
			if err != nil {
				t.Errorf("ParseNetworks error: %v", err)
			}
			tstEqual(t, "networks", networks, sample.networks)
			volumes, err := e.ParseVolumes(tstRead(t, v, "volumes.json"))
			if err != nil {
				t.Errorf("ParseVolumes error: %v", err)
			}
			tstEqual(t, "volumes", volumes, sample.volumes)
			images, err := e.ParseImages(tstRead(t, v, "images.json"))
			if err != nil {
				t.Errorf("ParseImages error: %v", err)
			}
			tstEqual(t, "images", images, sample.images)
		})
	}

	e := tstNew(t, "docker")
	t.Run("name key is case-insensitive and blank names are skipped", func(t *testing.T) {
		in := []byte("{\"Name\":\"a\"}\n{\"ID\":\"x\"}\n{\"name\":\"b\"}\n{\"Name\":\"\"}\n")
		networks, err := e.ParseNetworks(in)
		if err != nil {
			t.Errorf("ParseNetworks error: %v", err)
		}
		tstEqual(t, "networks", networks, []string{"a", "b"})
		volumes, err := e.ParseVolumes(in)
		if err != nil {
			t.Errorf("ParseVolumes error: %v", err)
		}
		tstEqual(t, "volumes", volumes, []string{"a", "b"})
	})
	t.Run("images skip none and blank halves", func(t *testing.T) {
		in := []byte(`[{"Repository":"r","Tag":"t"},{"Repository":"r","Tag":""},{"Repository":"","Tag":"t"},{"Repository":"<none>","Tag":"t"},{"Repository":"r","Tag":"<none>"},{"Names":["x:1","y:2"],"Repository":"ignored","Tag":"ignored"}]`)
		images, err := e.ParseImages(in)
		if err != nil {
			t.Errorf("ParseImages error: %v", err)
		}
		tstEqual(t, "images", images, []string{"r:t", "x:1", "y:2"})
	})
}

func TestParseInvalid(t *testing.T) {
	bad := []struct{ name, in string }{
		{"not json", "not json"},
		{"truncated", `[{"Name"`},
		{"number", "42"},
	}
	for _, v := range tstVariants {
		e := tstNew(t, v)
		for _, tt := range bad {
			t.Run(v+"/"+tt.name, func(t *testing.T) {
				in := []byte(tt.in)
				if got, err := e.ParseContainers(in); err == nil || got != nil {
					t.Errorf("ParseContainers(%q) = %v, %v; want nil, error", tt.in, got, err)
				}
				if got, err := e.ParseNetworks(in); err == nil || got != nil {
					t.Errorf("ParseNetworks(%q) = %v, %v; want nil, error", tt.in, got, err)
				}
				if got, err := e.ParseVolumes(in); err == nil || got != nil {
					t.Errorf("ParseVolumes(%q) = %v, %v; want nil, error", tt.in, got, err)
				}
				if got, err := e.ParseImages(in); err == nil || got != nil {
					t.Errorf("ParseImages(%q) = %v, %v; want nil, error", tt.in, got, err)
				}
				if _, err := e.ParseInspect(in); err == nil {
					t.Errorf("ParseInspect(%q) gave nil error", tt.in)
				}
			})
		}
	}
}

func TestParseBlank(t *testing.T) {
	blank := []struct{ name, in string }{{"empty", ""}, {"whitespace", " \n\t"}}
	for _, v := range tstVariants {
		e := tstNew(t, v)
		for _, tt := range blank {
			t.Run(v+"/"+tt.name, func(t *testing.T) {
				in, b := tt.in, []byte(tt.in)
				if got, err := e.ParseContainers(b); err != nil || got == nil || len(got) != 0 {
					t.Errorf("ParseContainers(%q) = %#v, %v; want an empty non-nil slice and nil error", in, got, err)
				}
				if got, err := e.ParseNetworks(b); err != nil || got == nil || len(got) != 0 {
					t.Errorf("ParseNetworks(%q) = %#v, %v; want an empty non-nil slice and nil error", in, got, err)
				}
				if got, err := e.ParseVolumes(b); err != nil || got == nil || len(got) != 0 {
					t.Errorf("ParseVolumes(%q) = %#v, %v; want an empty non-nil slice and nil error", in, got, err)
				}
				if got, err := e.ParseImages(b); err != nil || got == nil || len(got) != 0 {
					t.Errorf("ParseImages(%q) = %#v, %v; want an empty non-nil slice and nil error", in, got, err)
				}
				if _, err := e.ParseInspect(b); err == nil {
					t.Errorf("ParseInspect(%q) gave nil error", in)
				}
			})
		}
		t.Run(v+"/inspect empty array", func(t *testing.T) {
			for _, in := range []string{"[]", " [ ] \n"} {
				if _, err := e.ParseInspect([]byte(in)); err == nil {
					t.Errorf("ParseInspect(%q) gave nil error", in)
				}
			}
		})
	}
}

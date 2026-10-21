package dockercli_test

import (
	"reflect"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

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
	e := dockercli.New(tstFull())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.ParseContainers([]byte(tt.in))
			if err != nil || len(got) != 1 {
				t.Fatalf("ParseContainers(%s) = %d containers, %v; want 1, nil", tt.in, len(got), err)
			}
			c := got[0]
			tstEqual(t, "fields", tstFields{Name: c.Name, Status: c.Status, Health: c.Health, ExitCode: c.ExitCode, Service: c.Service, Number: c.Number}, tt.want)
		})
	}
}

func TestParseContainersShapes(t *testing.T) {
	e := dockercli.New(tstFull())
	t.Run("ndjson rows", func(t *testing.T) {
		got, err := e.ParseContainers([]byte("{\"Names\":\"a\",\"Image\":\"img:1\",\"Command\":\"\\\"nginx -g 'daemon off;'\\\"\"}\n{\"Names\":\"b\"}\n"))
		if err != nil || len(got) != 2 {
			t.Fatalf("ParseContainers = %d containers, %v; want 2, nil", len(got), err)
		}
		tstEqual(t, "names", []string{got[0].Name, got[1].Name}, []string{"a", "b"})
		tstEqual(t, "a.Image", got[0].Image, "img:1")
		tstEqual(t, "a.Command", got[0].Command, []string{"nginx", "-g", "'daemon", "off;'"})
	})
	t.Run("array rows", func(t *testing.T) {
		got, err := e.ParseContainers([]byte(`[{"Names":["a"],"Command":["nginx","-g","daemon off;"]},{"Names":["b"]}]`))
		if err != nil || len(got) != 2 {
			t.Fatalf("ParseContainers = %d containers, %v; want 2, nil", len(got), err)
		}
		tstEqual(t, "names", []string{got[0].Name, got[1].Name}, []string{"a", "b"})
		tstEqual(t, "a.Command", got[0].Command, []string{"nginx", "-g", "daemon off;"})
	})
}

func TestParseContainersLabels(t *testing.T) {
	e := dockercli.New(tstFull())
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
	e := dockercli.New(tstFull())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func TestParseInspect(t *testing.T) {
	e := dockercli.New(tstFull())
	t.Run("docker shape", func(t *testing.T) {
		got, err := e.ParseInspect([]byte(`[{"Name":"/p-web-1","Config":{"Image":"nginx:1.27","Labels":{"com.docker.compose.service":"web","com.docker.compose.container-number":"1"},"Cmd":["nginx","-g","daemon off;"]},"State":{"Status":"running","ExitCode":0,"Health":{"Status":"healthy"}}}]`))
		if err != nil {
			t.Fatalf("ParseInspect error: %v", err)
		}
		tstEqual(t, "Name", got.Name, "p-web-1")
		tstEqual(t, "Image", got.Image, "nginx:1.27")
		tstEqual(t, "Status", got.Status, plan.StatusRunning)
		tstEqual(t, "Health", got.Health, plan.HealthHealthy)
		tstEqual(t, "ExitCode", got.ExitCode, 0)
		tstEqual(t, "Service", got.Service, "web")
		tstEqual(t, "Number", got.Number, 1)
		tstEqual(t, "Labels[service]", got.Labels[plan.LabelService], "web")
		tstEqual(t, "Command", got.Command, []string{"nginx", "-g", "daemon off;"})
		tstEqual(t, "len(Ports)", len(got.Ports), 0)
	})
	t.Run("healthcheck key and image name fallback", func(t *testing.T) {
		got, err := e.ParseInspect([]byte(`{"Name":"p-web-1","ImageName":"docker.io/library/nginx:1.27","Config":{"Cmd":["sleep","1"]},"State":{"Status":"running","Healthcheck":{"Status":"starting"}}}`))
		if err != nil {
			t.Fatalf("ParseInspect error: %v", err)
		}
		tstEqual(t, "Name", got.Name, "p-web-1")
		tstEqual(t, "Image", got.Image, "docker.io/library/nginx:1.27")
		tstEqual(t, "Health", got.Health, plan.HealthStarting)
		tstEqual(t, "Command", got.Command, []string{"sleep", "1"})
		if got.Labels == nil || len(got.Labels) != 0 {
			t.Errorf("Labels = %#v, want an empty non-nil map", got.Labels)
		}
	})
	t.Run("exited without health", func(t *testing.T) {
		got, err := e.ParseInspect([]byte(`[{"Name":"/p-worker-2","Config":{"Image":"busybox:1.36"},"State":{"Status":"exited","ExitCode":1}}]`))
		if err != nil {
			t.Fatalf("ParseInspect error: %v", err)
		}
		tstEqual(t, "Status", got.Status, plan.StatusExited)
		tstEqual(t, "Health", got.Health, plan.HealthNone)
		tstEqual(t, "ExitCode", got.ExitCode, 1)
	})
	t.Run("first row wins", func(t *testing.T) {
		got, err := e.ParseInspect([]byte(`[{"Name":"/first","State":{"Status":"created"}},{"Name":"/second","State":{"Status":"running"}}]`))
		if err != nil {
			t.Fatalf("ParseInspect error: %v", err)
		}
		tstEqual(t, "Name", got.Name, "first")
		tstEqual(t, "Status", got.Status, plan.StatusCreated)
	})
}

func TestParseNames(t *testing.T) {
	e := dockercli.New(tstFull())
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
	e := dockercli.New(tstFull())
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
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
	t.Run("scalar names its token", func(t *testing.T) {
		_, err := e.ParseContainers([]byte("42"))
		if want := "want a JSON object or array, got 42"; err == nil || err.Error() != want {
			t.Errorf("ParseContainers(42) error = %v, want %q", err, want)
		}
	})
}

func TestParseBlank(t *testing.T) {
	blank := []struct{ name, in string }{{"empty", ""}, {"whitespace", " \n\t"}}
	e := dockercli.New(tstFull())
	for _, tt := range blank {
		t.Run(tt.name, func(t *testing.T) {
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
			if _, err := e.ParseInspect(b); err == nil || err.Error() != "inspect: no container in output" {
				t.Errorf("ParseInspect(%q) error = %v, want %q", in, err, "inspect: no container in output")
			}
		})
	}
	t.Run("inspect empty array", func(t *testing.T) {
		for _, in := range []string{"[]", " [ ] \n"} {
			got, err := e.ParseInspect([]byte(in))
			if err == nil || err.Error() != "inspect: no container in output" {
				t.Errorf("ParseInspect(%q) error = %v, want %q", in, err, "inspect: no container in output")
			}
			if !reflect.DeepEqual(got, plan.ContainerState{}) {
				t.Errorf("ParseInspect(%q) = %#v, want the zero ContainerState", in, got)
			}
		}
	})
}

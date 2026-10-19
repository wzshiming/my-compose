package applecontainer_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/applecontainer"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// The samples under testdata follow apple/container's JSON encoders:
//   - containers.json and inspect.json: ManagedContainer, i.e. {id, configuration: ContainerConfiguration, status: ContainerStatus{state, networks, startedDate}}
//   - container_status_string.json: the older ContainerSnapshot encoding, {configuration, status: "<state>", networks, startedDate}
//   - networks.json: NetworkResource {id, configuration: NetworkConfiguration{name, ...}, status: NetworkStatus}
//   - volumes.json: VolumeConfiguration {name, driver, format, source, createdAt, labels, options, sizeInBytes}
//   - images.json: ImageResource {id, configuration: {name, description, creationDate}, variants}
func tstRead(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tstWebState() plan.ContainerState {
	return plan.ContainerState{
		Name:     "proj-web-1",
		Service:  "web",
		Number:   1,
		Image:    "docker.io/library/nginx:1.27",
		Status:   plan.StatusRunning,
		Health:   plan.HealthNone,
		ExitCode: 0,
		Ports: []plan.Port{
			{HostIP: "127.0.0.1", Published: "8080", Target: 80, Protocol: "tcp"},
			{HostIP: "127.0.0.1", Published: "8081", Target: 81, Protocol: "tcp"},
		},
		Labels: map[string]string{
			plan.LabelContainerNumber: "1",
			plan.LabelProject:         "proj",
			plan.LabelService:         "web",
			"com.example.owner":       "platform",
		},
		Command: []string{"nginx", "-g", "daemon off;"},
	}
}

func tstDBState() plan.ContainerState {
	return plan.ContainerState{
		Name:     "proj-db-1",
		Service:  "db",
		Number:   1,
		Image:    "docker.io/library/postgres:16",
		Status:   plan.StatusExited,
		Health:   plan.HealthNone,
		ExitCode: -1,
		Labels: map[string]string{
			plan.LabelContainerNumber: "1",
			plan.LabelProject:         "proj",
			plan.LabelService:         "db",
		},
		Command: []string{"docker-entrypoint.sh", "postgres"},
	}
}

// tstEqualState compares every field, treating an absent Ports list and an empty one alike.
func tstEqualState(t *testing.T, got, want plan.ContainerState) {
	t.Helper()
	if len(got.Ports) == 0 && len(want.Ports) == 0 {
		got.Ports, want.Ports = nil, nil
	}
	for _, f := range []struct {
		name      string
		got, want any
	}{
		{"Name", got.Name, want.Name},
		{"Service", got.Service, want.Service},
		{"Number", got.Number, want.Number},
		{"Image", got.Image, want.Image},
		{"Status", got.Status, want.Status},
		{"Health", got.Health, want.Health},
		{"ExitCode", got.ExitCode, want.ExitCode},
		{"Ports", got.Ports, want.Ports},
		{"Labels", got.Labels, want.Labels},
		{"Command", got.Command, want.Command},
	} {
		if !reflect.DeepEqual(f.got, f.want) {
			t.Errorf("%s.%s = %+v, want %+v", want.Name, f.name, f.got, f.want)
		}
	}
}

func TestParseContainers(t *testing.T) {
	for _, file := range []string{"containers.json", "container_status_string.json"} {
		t.Run(file, func(t *testing.T) {
			got, err := applecontainer.New().ParseContainers(tstRead(t, file))
			if err != nil {
				t.Fatalf("ParseContainers: %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("ParseContainers returned %d containers, want 2: %+v", len(got), got)
			}
			tstEqualState(t, got[0], tstWebState())
			tstEqualState(t, got[1], tstDBState())
		})
	}
}

func TestParseContainersStatusMapping(t *testing.T) {
	cases := []struct {
		state    string
		status   string
		exitCode int
	}{
		{"running", plan.StatusRunning, 0},
		{"stopped", plan.StatusExited, -1},
		{"stopping", plan.StatusRunning, 0},
		{"created", plan.StatusUnknown, 0},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			in := `[{"id":"c","configuration":{"id":"c","image":{"reference":"img"}},"status":{"state":"` + tc.state + `","networks":[]}}]`
			got, err := applecontainer.New().ParseContainers([]byte(in))
			if err != nil || len(got) != 1 {
				t.Fatalf("ParseContainers = %+v, %v; want one container", got, err)
			}
			if got[0].Status != tc.status || got[0].ExitCode != tc.exitCode || got[0].Health != plan.HealthNone {
				t.Errorf("state %q parsed as Status %q, ExitCode %d, Health %q; want %q, %d, %q",
					tc.state, got[0].Status, got[0].ExitCode, got[0].Health, tc.status, tc.exitCode, plan.HealthNone)
			}
		})
	}
}

func TestParseContainersNameFallsBackToID(t *testing.T) {
	in := `[{"id":"abc","configuration":{"image":{"reference":"img"}},"status":{"state":"running","networks":[]}}]`
	got, err := applecontainer.New().ParseContainers([]byte(in))
	if err != nil || len(got) != 1 {
		t.Fatalf("ParseContainers = %+v, %v; want one container", got, err)
	}
	if got[0].Name != "abc" || got[0].Image != "img" {
		t.Errorf("Name, Image = %q, %q; want %q, %q", got[0].Name, got[0].Image, "abc", "img")
	}
}

func TestParseInspect(t *testing.T) {
	got, err := applecontainer.New().ParseInspect(tstRead(t, "inspect.json"))
	if err != nil {
		t.Fatalf("ParseInspect: %v", err)
	}
	tstEqualState(t, got, tstWebState())
}

func TestParseInspectErrors(t *testing.T) {
	for _, in := range []string{"", " \n\t", "not json", "42", "[]", `{"a":1}`} {
		if _, err := applecontainer.New().ParseInspect([]byte(in)); err == nil {
			t.Errorf("ParseInspect(%q) gave nil error", in)
		}
	}
}

func TestParseNames(t *testing.T) {
	e := applecontainer.New()
	cases := []struct {
		method string
		parse  func([]byte) ([]string, error)
		file   string
		want   []string
	}{
		{"ParseNetworks", e.ParseNetworks, "networks.json", []string{"default", "proj_front"}},
		{"ParseVolumes", e.ParseVolumes, "volumes.json", []string{"proj_data", "proj_logs"}},
		{"ParseImages", e.ParseImages, "images.json", []string{"docker.io/library/nginx:1.27", "docker.io/library/postgres:16"}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			got, err := tc.parse(tstRead(t, tc.file))
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s(%s) = %q, %v; want %q, nil", tc.method, tc.file, got, err, tc.want)
			}
		})
	}
}

func TestParseNameFallbacks(t *testing.T) {
	e := applecontainer.New()
	cases := []struct {
		method string
		parse  func([]byte) ([]string, error)
		in     string
		want   []string
	}{
		{"ParseNetworks", e.ParseNetworks, `[{"configuration":{"name":"x"}},{"id":"","configuration":{}}]`, []string{"x"}},
		{"ParseVolumes", e.ParseVolumes, `[{"id":"a"},{"configuration":{"name":"b"}},{"name":"c"},{}]`, []string{"a", "b", "c"}},
		{"ParseImages", e.ParseImages, `[{"configuration":{"name":"i"}},{"id":"sha256:abc","configuration":{}}]`, []string{"i"}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			got, err := tc.parse([]byte(tc.in))
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s(%s) = %q, %v; want %q, nil", tc.method, tc.in, got, err, tc.want)
			}
		})
	}
}

func TestParseBlank(t *testing.T) {
	e := applecontainer.New()
	for _, in := range []string{"", " \n\t"} {
		if got, err := e.ParseContainers([]byte(in)); got == nil || len(got) != 0 || err != nil {
			t.Errorf("ParseContainers(%q) = %#v, %v; want an empty non-nil slice and nil error", in, got, err)
		}
		for _, m := range []struct {
			name  string
			parse func([]byte) ([]string, error)
		}{{"ParseNetworks", e.ParseNetworks}, {"ParseVolumes", e.ParseVolumes}, {"ParseImages", e.ParseImages}} {
			if got, err := m.parse([]byte(in)); got == nil || len(got) != 0 || err != nil {
				t.Errorf("%s(%q) = %#v, %v; want an empty non-nil slice and nil error", m.name, in, got, err)
			}
		}
	}
}

func TestParseInvalid(t *testing.T) {
	e := applecontainer.New()
	for _, in := range []string{"not json", "42", `{"a":1}`, `[1,`} {
		if got, err := e.ParseContainers([]byte(in)); got != nil || err == nil {
			t.Errorf("ParseContainers(%q) = %#v, %v; want nil slice and an error", in, got, err)
		}
		for _, m := range []struct {
			name  string
			parse func([]byte) ([]string, error)
		}{{"ParseNetworks", e.ParseNetworks}, {"ParseVolumes", e.ParseVolumes}, {"ParseImages", e.ParseImages}} {
			if got, err := m.parse([]byte(in)); got != nil || err == nil {
				t.Errorf("%s(%q) = %#v, %v; want nil slice and an error", m.name, in, got, err)
			}
		}
	}
}

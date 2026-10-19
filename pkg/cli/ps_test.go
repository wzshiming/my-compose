package cli

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

const tstPsYAML = "services:\n  web:\n    image: nginx:1.27\n  db:\n    image: postgres:16\n"

var tstPsHeader = []string{"NAME", "IMAGE", "SERVICE", "STATUS", "PORTS"}

// tstPsContainers lists project p out of order: db running, web-1 healthy with ports, web-2 exited, web-3 created.
func tstPsContainers() []plan.ContainerState {
	web1 := tstContainer("p", "web", 1, plan.StatusRunning, plan.HealthHealthy)
	web1.Image = "nginx:1.27"
	web1.Command = []string{"nginx", "-g", "daemon off;"}
	web1.Ports = []plan.Port{
		{HostIP: "0.0.0.0", Published: "8080", Target: 80, Protocol: "tcp"},
		{Target: 443, Protocol: "tcp"},
	}
	web2 := tstContainer("p", "web", 2, plan.StatusExited, plan.HealthUnhealthy)
	web2.Image = "nginx:1.27"
	web2.ExitCode = 1
	web3 := tstContainer("p", "web", 3, plan.StatusCreated, "")
	web3.Image = "nginx:1.27"
	db := tstContainer("p", "db", 1, plan.StatusRunning, plan.HealthNone)
	db.Image = "postgres:16"
	db.Ports = []plan.Port{
		{Published: "5432", Target: 5432, Protocol: "tcp"},
		{Published: "9000", Target: 9000},
	}
	return []plan.ContainerState{web3, web1, db, web2}
}

var tstColumnGap = regexp.MustCompile(" {3,}")

// tstTable splits ps output into rows of five columns: tabwriter pads columns with at least three spaces and cell values only contain single spaces.
func tstTable(t *testing.T, out string) [][]string {
	t.Helper()
	var rows [][]string
	for _, line := range tstLines(out) {
		cols := tstColumnGap.Split(strings.TrimRight(line, " "), -1)
		if len(cols) > len(tstPsHeader) {
			t.Fatalf("line %q splits into %d columns, want at most %d", line, len(cols), len(tstPsHeader))
		}
		for len(cols) < len(tstPsHeader) {
			cols = append(cols, "")
		}
		rows = append(rows, cols)
	}
	return rows
}

func tstPs(t *testing.T, containers []plan.ContainerState, args ...string) (code int, out, errb string, f *tstEngine) {
	t.Helper()
	dir := tstProject(t, tstPsYAML)
	f = tstFake("fake")
	f.Containers = containers
	code, out, errb = tstRun(t, tstApp(f), tstArgs(dir, args...)...)
	return code, out, errb, f
}

func TestPsTable(t *testing.T) {
	tstIsolateEnv(t)
	all := [][]string{
		tstPsHeader,
		{"p-db-1", "postgres:16", "db", "running", "5432->5432/tcp, 9000->9000"},
		{"p-web-1", "nginx:1.27", "web", "running (healthy)", "0.0.0.0:8080->80/tcp, 443/tcp"},
		{"p-web-2", "nginx:1.27", "web", "exited (1)", ""},
		{"p-web-3", "nginx:1.27", "web", "created", ""},
	}
	for _, tt := range []struct {
		name       string
		containers []plan.ContainerState
		args       []string
		rows       [][]string
	}{
		{"hides exited and created, sorted by service and number", tstPsContainers(), []string{"ps"}, [][]string{all[0], all[1], all[2]}},
		{"-a lists every container", tstPsContainers(), []string{"ps", "-a"}, all},
		{"header without containers", nil, []string{"ps"}, [][]string{all[0]}},
		{"service argument filters", tstPsContainers(), []string{"ps", "web"}, [][]string{all[0], all[2]}},
		{"service argument with -a", tstPsContainers(), []string{"ps", "-a", "web"}, [][]string{all[0], all[2], all[3], all[4]}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errb, f := tstPs(t, tt.containers, tt.args...)
			if code != 0 || errb != "" {
				t.Fatalf("exit %d, stderr %q", code, errb)
			}
			if rows := tstTable(t, out); !reflect.DeepEqual(rows, tt.rows) {
				t.Errorf("table rows\n  %q\nwant\n  %q\noutput:\n%s", rows, tt.rows, out)
			}
			if kinds := tstKinds(f.Rendered); !reflect.DeepEqual(kinds, []string{"ListContainers"}) {
				t.Errorf("rendered %q, want only the ListContainers query", kinds)
			}
		})
	}
}

func TestPsJSON(t *testing.T) {
	tstIsolateEnv(t)
	code, out, errb, _ := tstPs(t, tstPsContainers(), "ps", "-a", "--format", "json")
	if code != 0 || errb != "" {
		t.Fatalf("exit %d, stderr %q", code, errb)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	want := []map[string]any{
		{
			"name": "p-db-1", "service": "db", "number": 1.0, "image": "postgres:16",
			"status": "running", "health": "none", "exit_code": 0.0,
			"ports": []any{
				map[string]any{"host_ip": "", "published": "5432", "target": 5432.0, "protocol": "tcp"},
				map[string]any{"host_ip": "", "published": "9000", "target": 9000.0, "protocol": ""},
			},
			"command": []any{},
		},
		{
			"name": "p-web-1", "service": "web", "number": 1.0, "image": "nginx:1.27",
			"status": "running", "health": "healthy", "exit_code": 0.0,
			"ports": []any{
				map[string]any{"host_ip": "0.0.0.0", "published": "8080", "target": 80.0, "protocol": "tcp"},
				map[string]any{"host_ip": "", "published": "", "target": 443.0, "protocol": "tcp"},
			},
			"command": []any{"nginx", "-g", "daemon off;"},
		},
		{
			"name": "p-web-2", "service": "web", "number": 2.0, "image": "nginx:1.27",
			"status": "exited", "health": "unhealthy", "exit_code": 1.0,
			"ports": []any{}, "command": []any{},
		},
		{
			"name": "p-web-3", "service": "web", "number": 3.0, "image": "nginx:1.27",
			"status": "created", "health": "", "exit_code": 0.0,
			"ports": []any{}, "command": []any{},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded JSON\n  %v\nwant\n  %v\noutput:\n%s", got, want, out)
	}
	if !strings.HasSuffix(out, "\n") || !strings.Contains(out, "\n  {\n") || !strings.Contains(out, "\n    \"name\": \"p-db-1\"") {
		t.Errorf("JSON is not indented by two spaces per level with a final newline:\n%s", out)
	}

	code, out, errb, _ = tstPs(t, nil, "ps", "--format", "json")
	if code != 0 || out != "[]\n" || errb != "" {
		t.Errorf("without containers: exit %d, stdout %q, stderr %q; want 0 and \"[]\\n\"", code, out, errb)
	}
}

func TestPsQuiet(t *testing.T) {
	tstIsolateEnv(t)
	for _, tt := range []struct {
		name       string
		containers []plan.ContainerState
		args       []string
		out        string
	}{
		{"names only", tstPsContainers(), []string{"ps", "-a", "-q"}, "p-db-1\np-web-1\np-web-2\np-web-3\n"},
		{"-q wins over --format", tstPsContainers(), []string{"ps", "-q", "--format", "json"}, "p-db-1\np-web-1\n"},
		{"nothing without containers", nil, []string{"ps", "-q"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errb, _ := tstPs(t, tt.containers, tt.args...)
			if code != 0 || out != tt.out || errb != "" {
				t.Errorf("exit %d, stdout %q, stderr %q; want 0 and %q", code, out, errb, tt.out)
			}
		})
	}
}

func TestPsErrors(t *testing.T) {
	tstIsolateEnv(t)
	code, out, errb, _ := tstPs(t, tstPsContainers(), "ps", "nosuch")
	if code != 1 || out != "" || errb != "error: no such service: nosuch\n" {
		t.Errorf("ps nosuch: exit %d, stdout %q, stderr %q; want 1 and the no such service error", code, out, errb)
	}
}

func TestPsDryRun(t *testing.T) {
	tstIsolateEnv(t)
	dir := tstProject(t, tstPsYAML)
	fake := enginetest.NewFake("fake")
	a := tstApp()
	a.candidates = append(a.candidates, fake)
	for _, args := range [][]string{
		tstArgs(dir, "--dry-run", "ps"),
		tstArgs(dir, "--dry-run", "ps", "-q", "--format", "json"),
	} {
		code, out, errb := tstRun(t, a, args...)
		if code != 0 || out != "fake ListContainers p\n" || errb != "" {
			t.Errorf("%q: exit %d, stdout %q, stderr %q; want 0 and the ps command line", args, code, out, errb)
		}
	}
}

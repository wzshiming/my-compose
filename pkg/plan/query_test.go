package plan

import (
	"reflect"
	"testing"
)

func TestPs(t *testing.T) {
	plan, _, err := Ps(loadProject(t, `{services: {web: {image: nginx}}}`))
	if err != nil || plan == nil {
		t.Fatalf("Ps = %v, %v; want a plan", plan, err)
	}
	if want := []Step{ListContainers{Project: "p"}}; !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("Ps steps = %#v, want %#v", plan.Steps, want)
	}
}

func TestStateQueries(t *testing.T) {
	plan, _, err := StateQueries(loadProject(t, `{services: {web: {image: nginx}}}`))
	if err != nil || plan == nil {
		t.Fatalf("StateQueries = %v, %v; want a plan", plan, err)
	}
	want := []Step{ListContainers{Project: "p"}, ListNetworks{}, ListVolumes{}, ListImages{}}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Errorf("StateQueries steps = %#v, want %#v", plan.Steps, want)
	}
}

func TestAC11ServiceLogs(t *testing.T) {
	const yaml = `{services: {web: {image: nginx, scale: 2, depends_on: [db]}, db: {image: postgres}}}`
	// ghost has no service in the file.
	state := State{Live: true, Containers: []ContainerState{
		stateContainer("web", 2, StatusRunning),
		stateContainer("ghost", 1, StatusRunning),
		stateContainer("db", 1, StatusRunning),
		stateContainer("web", 1, StatusRunning),
	}}
	logs := func(t *testing.T, s State, opts LogsOptions) []Step {
		t.Helper()
		plan, _, err := ServiceLogs(loadProject(t, yaml), s, opts)
		if err != nil || plan == nil {
			t.Fatalf("ServiceLogs = %v, %v; want a plan", plan, err)
		}
		return plan.Steps
	}
	t.Run("no services selects every file service's state containers by service then number, passing follow and tail", func(t *testing.T) {
		got := logs(t, state, LogsOptions{Follow: true, Tail: "10"})
		want := []Step{
			Logs{Name: "p-db-1", Follow: true, Tail: "10"},
			Logs{Name: "p-web-1", Follow: true, Tail: "10"},
			Logs{Name: "p-web-2", Follow: true, Tail: "10"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ServiceLogs steps = %#v, want %#v", got, want)
		}
	})
	t.Run("unknown service is an error", func(t *testing.T) {
		if _, _, err := ServiceLogs(loadProject(t, yaml), state, LogsOptions{Services: []string{"nope"}}); err == nil {
			t.Error("ServiceLogs succeeded, want an error for unknown service nope")
		}
	})
	t.Run("empty state derives the named service's replicas from the file without its dependencies", func(t *testing.T) {
		got := logs(t, State{}, LogsOptions{Services: []string{"web"}})
		if want := []Step{Logs{Name: "p-web-1"}, Logs{Name: "p-web-2"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("ServiceLogs steps = %#v, want %#v", got, want)
		}
	})
	t.Run("named service skips other services' containers", func(t *testing.T) {
		got := logs(t, state, LogsOptions{Services: []string{"db"}})
		if want := []Step{Logs{Name: "p-db-1"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("ServiceLogs steps = %#v, want %#v", got, want)
		}
	})
}

func TestG1LiveStateServiceLogs(t *testing.T) {
	logs := func(t *testing.T, s State) []Step {
		t.Helper()
		plan, _, err := ServiceLogs(loadProject(t, `{services: {web: {image: nginx}}}`), s, LogsOptions{})
		if err != nil || plan == nil {
			t.Fatalf("ServiceLogs = %v, %v; want a plan", plan, err)
		}
		return plan.Steps
	}
	t.Run("live state without containers plans nothing", func(t *testing.T) {
		if got := logs(t, State{Live: true}); len(got) != 0 {
			t.Errorf("ServiceLogs steps = %#v, want none", got)
		}
	})
	t.Run("zero state derives the containers from the file", func(t *testing.T) {
		if got, want := logs(t, State{}), []Step{Logs{Name: "p-web-1"}}; !reflect.DeepEqual(got, want) {
			t.Errorf("ServiceLogs steps = %#v, want %#v", got, want)
		}
	})
}

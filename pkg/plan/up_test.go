package plan

import (
	"slices"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

const idempotentYAML = `
services:
  web:
    image: nginx
    networks: [front]
    volumes: ["data:/data"]
  api:
    build: .
    depends_on: [web]
networks:
  front: {}
volumes:
  data: {}
`

func convergedState() State {
	return State{
		Live:       true,
		Containers: []ContainerState{stateContainer("web", 1, StatusRunning), stateContainer("api", 1, StatusRunning)},
		Networks:   []string{"p_default", "p_front"},
		Volumes:    []string{"p_data"},
		Images:     []string{"docker.io/library/nginx:latest", "docker.io/library/p-api:latest"},
	}
}

func TestAC5Idempotent(t *testing.T) {
	t.Run("converged state plans nothing", func(t *testing.T) {
		if plan, _ := mustUp(t, loadProject(t, idempotentYAML), State{}, healthCaps, UpOptions{}); len(plan.Steps) == 0 {
			t.Fatal("Up from an empty State planned no steps")
		}
		if plan, _ := mustUp(t, loadProject(t, idempotentYAML), convergedState(), healthCaps, UpOptions{}); len(plan.Steps) != 0 {
			t.Errorf("Up from a converged State = %v, want no steps", describePlan(plan))
		}
	})
	t.Run("exited container is only started", func(t *testing.T) {
		s := convergedState()
		s.Containers[0].Status = StatusExited
		plan, _ := mustUp(t, loadProject(t, idempotentYAML), s, healthCaps, UpOptions{})
		if got, want := describePlan(plan), []string{"StartContainers p-web-1"}; !slices.Equal(got, want) {
			t.Errorf("steps = %v, want %v", got, want)
		}
	})
	t.Run("missing network is only created", func(t *testing.T) {
		s := convergedState()
		s.Networks = []string{"p_default"}
		plan, _ := mustUp(t, loadProject(t, idempotentYAML), s, healthCaps, UpOptions{})
		if got, want := describePlan(plan), []string{"CreateNetwork p_front"}; !slices.Equal(got, want) {
			t.Errorf("steps = %v, want %v", got, want)
		}
	})
}

func TestAC6BuildTrigger(t *testing.T) {
	imagePresent := State{Live: true, Images: []string{"docker.io/library/nginx:latest"}}
	localhostBuilt := State{Live: true, Images: []string{"localhost/p-app:latest"}}
	tests := []struct {
		name  string
		yaml  string
		state State
		opts  UpOptions
		want  []string
	}{
		{"present image is not rebuilt", `{services: {web: {build: ., image: nginx}}}`, imagePresent, UpOptions{}, nil},
		{"empty state builds", `{services: {web: {build: ., image: nginx}}}`, State{}, UpOptions{}, []string{"nginx"}},
		{"pull_policy build rebuilds a present image", `{services: {web: {build: ., image: nginx, pull_policy: build}}}`, imagePresent, UpOptions{}, []string{"nginx"}},
		{"Build option rebuilds a present image", `{services: {web: {build: ., image: nginx}}}`, imagePresent, UpOptions{Build: true}, []string{"nginx"}},
		{"shared tag is built once", `{services: {a: {build: ., image: shared}, b: {build: ., image: shared}}}`, State{}, UpOptions{}, []string{"shared"}},
		{"built image listed under localhost/ is not rebuilt", `{services: {app: {build: .}}}`, localhostBuilt, UpOptions{}, nil},
		{"built image missing from a live state is built", `{services: {app: {build: .}}}`, State{Live: true}, UpOptions{}, []string{"p-app"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, _ := mustUp(t, loadProject(t, tt.yaml), tt.state, healthCaps, tt.opts)
			if len(stepsOf[RunContainer](plan)) == 0 {
				t.Fatalf("steps = %v, want RunContainer steps", describePlan(plan))
			}
			if got := stepArgs(plan, "BuildImage"); !slices.Equal(got, tt.want) {
				t.Errorf("BuildImage tags = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAC7DependsOn(t *testing.T) {
	dependsYAML := func(condition, dependency string) string {
		return `{services: {a: {image: alpine, depends_on: {b: {condition: ` + condition + `}}}, b: {image: alpine` + dependency + `}}}`
	}
	const withHealthcheck = `, healthcheck: {test: [CMD, "true"]}`
	t.Run("healthy waits between the dependency and the dependent", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, dependsYAML("service_healthy", withHealthcheck)), State{}, healthCaps, UpOptions{})
		want := []string{"CreateNetwork p_default", "RunContainer p-b-1", "WaitContainer p-b-1 healthy", "RunContainer p-a-1"}
		if got := describePlan(plan); !slices.Equal(got, want) {
			t.Errorf("steps = %v, want %v", got, want)
		}
	})
	degraded := []struct {
		name, dependency string
		caps             Capabilities
	}{
		{"healthy degrades without the healthcheck capability", withHealthcheck, Capabilities{}},
		{"healthy degrades when the dependency disables its healthcheck", `, healthcheck: {disable: true}`, healthCaps},
		{"healthy degrades when the dependency tests NONE", `, healthcheck: {test: [NONE]}`, healthCaps},
	}
	for _, tt := range degraded {
		t.Run(tt.name, func(t *testing.T) {
			plan, ws := mustUp(t, loadProject(t, dependsYAML("service_healthy", tt.dependency)), State{}, tt.caps, UpOptions{})
			if waits := stepArgs(plan, "WaitContainer"); len(waits) != 0 {
				t.Errorf("WaitContainer steps = %v, want none", waits)
			}
			expectWarning(t, ws, "services.a", "depends_on.b.condition", "degraded to service_started (")
		})
	}
	t.Run("healthy waits when the dependency declares no healthcheck", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, dependsYAML("service_healthy", "")), State{}, healthCaps, UpOptions{})
		if got, want := stepArgs(plan, "WaitContainer"), []string{"p-b-1 healthy"}; !slices.Equal(got, want) {
			t.Errorf("WaitContainer steps = %v, want %v", got, want)
		}
	})
	t.Run("completed waits without the healthcheck capability", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, dependsYAML("service_completed_successfully", "")), State{}, Capabilities{}, UpOptions{})
		want := []string{"CreateNetwork p_default", "RunContainer p-b-1", "WaitContainer p-b-1 completed", "RunContainer p-a-1"}
		if got := describePlan(plan); !slices.Equal(got, want) {
			t.Errorf("steps = %v, want %v", got, want)
		}
	})
	t.Run("one wait per dependency replica", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, dependsYAML("service_healthy", withHealthcheck+", scale: 2")), State{}, healthCaps, UpOptions{})
		if got, want := sortedCopy(stepArgs(plan, "WaitContainer")), []string{"p-b-1 healthy", "p-b-2 healthy"}; !slices.Equal(got, want) {
			t.Errorf("WaitContainer steps = %v, want %v", got, want)
		}
	})
	t.Run("shared dependency is waited for once", func(t *testing.T) {
		const yaml = `{services: {a: {image: alpine, depends_on: {b: {condition: service_healthy}}}, c: {image: alpine, depends_on: {b: {condition: service_healthy}}}, b: {image: alpine` + withHealthcheck + `}}}`
		plan, _ := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
		if got, want := stepArgs(plan, "WaitContainer"), []string{"p-b-1 healthy"}; !slices.Equal(got, want) {
			t.Errorf("WaitContainer steps = %v, want %v", got, want)
		}
	})
}

func TestUpServicesScope(t *testing.T) {
	const chainYAML = `{services: {a: {image: alpine, depends_on: [b]}, b: {image: alpine, depends_on: [c]}, c: {image: alpine}, d: {image: alpine, networks: [dnet]}}, networks: {dnet: {}}}`
	t.Run("named services include transitive dependencies only", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, chainYAML), State{}, healthCaps, UpOptions{Services: []string{"a"}})
		if got, want := sortedCopy(stepArgs(plan, "RunContainer")), []string{"p-a-1", "p-b-1", "p-c-1"}; !slices.Equal(got, want) {
			t.Errorf("RunContainer names = %v, want %v", got, want)
		}
	})
	t.Run("only networks of services in scope are created", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, chainYAML), State{}, healthCaps, UpOptions{Services: []string{"a"}})
		if got, want := stepArgs(plan, "CreateNetwork"), []string{"p_default"}; !slices.Equal(got, want) {
			t.Errorf("CreateNetwork names = %v, want %v", got, want)
		}
	})
	t.Run("unknown service is an error", func(t *testing.T) {
		if _, _, err := Up(loadProject(t, chainYAML), State{}, healthCaps, UpOptions{Services: []string{"nope"}}); err == nil {
			t.Error("Up succeeded, want an error for unknown service nope")
		}
	})
	t.Run("required dependency on a missing service is an error", func(t *testing.T) {
		p := loadProject(t, chainYAML)
		editService(p, "c", func(s *types.ServiceConfig) {
			s.DependsOn = types.DependsOnConfig{"ghost": {Condition: types.ServiceConditionStarted, Required: true}}
		})
		if _, _, err := Up(p, State{}, healthCaps, UpOptions{}); err == nil {
			t.Error("Up succeeded, want an error for the required dependency on ghost")
		}
	})
	const optionalYAML = `
services:
  a:
    image: alpine
    depends_on:
      opt:
        condition: service_started
        required: false
  opt:
    image: alpine
    profiles: [debug]
`
	for _, tt := range []struct {
		name     string
		services []string
	}{{"all services", nil}, {"named service", []string{"a"}}} {
		t.Run("optional dependency on a disabled service is skipped for "+tt.name, func(t *testing.T) {
			plan, _ := mustUp(t, loadProject(t, optionalYAML), State{}, healthCaps, UpOptions{Services: tt.services})
			if got, want := stepArgs(plan, "RunContainer"), []string{"p-a-1"}; !slices.Equal(got, want) {
				t.Errorf("RunContainer names = %v, want %v", got, want)
			}
		})
	}
}

func TestUpReconcile(t *testing.T) {
	const yaml = `{services: {web: {image: nginx, scale: 3}}}`
	ctrs := func(statuses map[int]string) State {
		s := State{Live: true, Networks: []string{"p_default"}}
		for n := 1; n <= 4; n++ {
			if status, ok := statuses[n]; ok {
				s.Containers = append(s.Containers, stateContainer("web", n, status))
			}
		}
		slices.Reverse(s.Containers)
		return s
	}
	tests := []struct {
		name  string
		state State
		want  []string
	}{
		{"surplus replica is stopped and removed",
			ctrs(map[int]string{1: StatusRunning, 2: StatusRunning, 3: StatusRunning, 4: StatusRunning}),
			[]string{"StopContainers p-web-4", "RemoveContainers p-web-4"}},
		{"stopped replicas are each started by number",
			ctrs(map[int]string{1: StatusCreated, 2: StatusUnknown, 3: StatusExited}),
			[]string{"StartContainers p-web-1", "StartContainers p-web-2", "StartContainers p-web-3"}},
		{"dead replica is removed then run",
			ctrs(map[int]string{1: StatusRunning, 2: StatusDead, 3: StatusRunning}),
			[]string{"RemoveContainers p-web-2", "RunContainer p-web-2"}},
		{"missing replica is run while paused and restarting ones are kept",
			ctrs(map[int]string{1: StatusPaused, 2: StatusRestarting}),
			[]string{"RunContainer p-web-3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, _ := mustUp(t, loadProject(t, yaml), tt.state, healthCaps, UpOptions{})
			if got := describePlan(plan); !slices.Equal(got, tt.want) {
				t.Errorf("steps = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpExternalResources(t *testing.T) {
	const yaml = `
services:
  web:
    image: nginx
    networks: [ext, own]
    volumes: ["extvol:/a", "ownvol:/b"]
networks:
  ext: {external: true}
  own: {}
volumes:
  extvol: {external: true}
  ownvol: {}
`
	plan, _ := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
	if got, want := stepArgs(plan, "CreateNetwork"), []string{"p_own"}; !slices.Equal(got, want) {
		t.Errorf("CreateNetwork names = %v, want %v", got, want)
	}
	if got, want := stepArgs(plan, "CreateVolume"), []string{"p_ownvol"}; !slices.Equal(got, want) {
		t.Errorf("CreateVolume names = %v, want %v", got, want)
	}
}

func TestG1StaleStopFilter(t *testing.T) {
	const yaml = `{services: {web: {image: nginx}}}`
	// surplus keeps replica 1 running and adds replicas 2.. with the given statuses.
	surplus := func(statuses ...string) State {
		s := State{Live: true, Networks: []string{"p_default"}, Containers: []ContainerState{stateContainer("web", 1, StatusRunning)}}
		for i, status := range statuses {
			s.Containers = append(s.Containers, stateContainer("web", i+2, status))
		}
		return s
	}
	tests := []struct {
		name  string
		state State
		want  []string
	}{
		{"exited, created and dead surplus replicas are removed without being stopped",
			surplus(StatusRunning, StatusExited, StatusPaused, StatusCreated, StatusDead),
			[]string{"StopContainers p-web-2,p-web-4", "RemoveContainers p-web-2,p-web-3,p-web-4,p-web-5,p-web-6"}},
		{"no stop step when every surplus replica is already stopped",
			surplus(StatusExited, StatusCreated, StatusDead),
			[]string{"RemoveContainers p-web-2,p-web-3,p-web-4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, _ := mustUp(t, loadProject(t, yaml), tt.state, healthCaps, UpOptions{})
			if got := describePlan(plan); !slices.Equal(got, tt.want) {
				t.Errorf("steps = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestG1WaitGating(t *testing.T) {
	const yaml = `{services: {a: {image: alpine, depends_on: {b: {condition: service_healthy}}}, b: {image: alpine, healthcheck: {test: [CMD, "true"]}}}}`
	state := State{
		Live:       true,
		Containers: []ContainerState{stateContainer("a", 1, StatusRunning), stateContainer("b", 1, StatusRunning)},
		Networks:   []string{"p_default"},
	}
	plan, _ := mustUp(t, loadProject(t, yaml), state, healthCaps, UpOptions{})
	if got := describePlan(plan); len(got) != 0 {
		t.Errorf("steps = %v, want none when the dependent has nothing to start or run", got)
	}
}

func TestS5PerContainerStart(t *testing.T) {
	state := State{Live: true, Networks: []string{"p_default"}}
	for _, n := range []int{2, 3, 1} {
		state.Containers = append(state.Containers, stateContainer("web", n, StatusExited))
	}
	plan, _ := mustUp(t, loadProject(t, `{services: {web: {image: nginx, scale: 3}}}`), state, healthCaps, UpOptions{})
	want := []string{"StartContainers p-web-1", "StartContainers p-web-2", "StartContainers p-web-3"}
	if got := describePlan(plan); !slices.Equal(got, want) {
		t.Errorf("steps = %v, want one StartContainers per stopped replica by number: %v", got, want)
	}
}

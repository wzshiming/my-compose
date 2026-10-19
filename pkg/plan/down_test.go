package plan

import (
	"slices"
	"strings"
	"testing"
	"time"
)

const downYAML = `
services:
  front:
    image: nginx
    depends_on: [api]
    stop_grace_period: 30s
    networks: [web, ext]
  api:
    image: alpine
    depends_on: [db]
  db:
    image: postgres
    scale: 2
    volumes: ["data:/data", "shared:/shared"]
  admin:
    image: alpine
networks:
  web: {}
  ext: {external: true}
volumes:
  data: {}
  shared: {external: true}
`

// downState holds every file service plus the orphan services ghost (running) and zombie (exited).
func downState() State {
	return State{
		Live: true,
		Containers: []ContainerState{
			stateContainer("ghost", 1, StatusRunning),
			stateContainer("db", 2, StatusExited),
			stateContainer("front", 1, StatusRunning),
			stateContainer("zombie", 1, StatusExited),
			stateContainer("admin", 1, StatusRunning),
			stateContainer("db", 1, StatusRunning),
			stateContainer("api", 1, StatusRunning),
		},
		Networks: []string{"p_web", "ext", "p_default"},
		Volumes:  []string{"shared", "p_data"},
	}
}

func stopTimeoutOf(t *testing.T, plan *Plan, name string) *time.Duration {
	t.Helper()
	for _, s := range stepsOf[StopContainers](plan) {
		if slices.Contains(s.Names, name) {
			return s.Timeout
		}
	}
	t.Fatalf("no StopContainers for %s in %v", name, describePlan(plan))
	return nil
}

func durationString(d *time.Duration) string {
	if d == nil {
		return "nil"
	}
	return d.String()
}

func TestAC9Down(t *testing.T) {
	down := func(t *testing.T, s State, opts DownOptions) *Plan {
		t.Helper()
		return mustDown(t, loadProject(t, downYAML), s, opts)
	}
	t.Run("file services in reverse dependency order", func(t *testing.T) {
		got := stepArgs(down(t, downState(), DownOptions{}), "RemoveContainers")
		want := []string{"p-front-1", "p-api-1", "p-db-1,p-db-2", "p-admin-1"}
		if len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
			t.Errorf("RemoveContainers = %v, want it to start with %v", got, want)
		}
	})
	t.Run("orphan services last, by name", func(t *testing.T) {
		got := stepArgs(down(t, downState(), DownOptions{}), "RemoveContainers")
		want := []string{"p-ghost-1", "p-zombie-1"}
		if len(got) < len(want) || !slices.Equal(got[len(got)-len(want):], want) {
			t.Errorf("RemoveContainers = %v, want it to end with %v", got, want)
		}
	})
	t.Run("each group stops its running containers then removes all of them", func(t *testing.T) {
		var got []string
		for _, d := range describePlan(down(t, downState(), DownOptions{})) {
			if strings.HasPrefix(d, "StopContainers ") || strings.HasPrefix(d, "RemoveContainers ") {
				got = append(got, d)
			}
		}
		want := []string{
			"StopContainers p-front-1", "RemoveContainers p-front-1",
			"StopContainers p-api-1", "RemoveContainers p-api-1",
			"StopContainers p-db-1", "RemoveContainers p-db-1,p-db-2",
			"StopContainers p-admin-1", "RemoveContainers p-admin-1",
			"StopContainers p-ghost-1", "RemoveContainers p-ghost-1",
			"RemoveContainers p-zombie-1",
		}
		if !slices.Equal(got, want) {
			t.Errorf("container steps = %v, want %v", got, want)
		}
	})
	t.Run("state networks except external ones are removed", func(t *testing.T) {
		got := stepArgs(down(t, downState(), DownOptions{}), "RemoveNetwork")
		if want := []string{"p_default", "p_web"}; !slices.Equal(got, want) {
			t.Errorf("RemoveNetwork = %v, want %v", got, want)
		}
	})
	t.Run("volumes are kept without the Volumes option", func(t *testing.T) {
		plan := down(t, downState(), DownOptions{})
		if got := stepArgs(plan, "RemoveVolume"); len(got) != 0 {
			t.Errorf("RemoveVolume = %v, want none", got)
		}
		removes := stepsOf[RemoveContainers](plan)
		if len(removes) == 0 {
			t.Fatalf("steps = %v, want RemoveContainers steps", describePlan(plan))
		}
		for _, r := range removes {
			if r.Volumes {
				t.Errorf("RemoveContainers %v has Volumes = true, want false", r.Names)
			}
		}
	})
	t.Run("state volumes except external ones are removed with the Volumes option", func(t *testing.T) {
		plan := down(t, downState(), DownOptions{Volumes: true})
		if got, want := stepArgs(plan, "RemoveVolume"), []string{"p_data"}; !slices.Equal(got, want) {
			t.Errorf("RemoveVolume = %v, want %v", got, want)
		}
		for _, r := range stepsOf[RemoveContainers](plan) {
			if !r.Volumes {
				t.Errorf("RemoveContainers %v has Volumes = false, want true", r.Names)
			}
		}
	})
	t.Run("timeout defaults to stop_grace_period", func(t *testing.T) {
		plan := down(t, downState(), DownOptions{})
		if got := stopTimeoutOf(t, plan, "p-front-1"); got == nil || *got != 30*time.Second {
			t.Errorf("front stop timeout = %s, want 30s", durationString(got))
		}
		for _, name := range []string{"p-api-1", "p-ghost-1"} {
			if got := stopTimeoutOf(t, plan, name); got != nil {
				t.Errorf("%s stop timeout = %s, want nil", name, durationString(got))
			}
		}
	})
	t.Run("explicit timeout applies to every group", func(t *testing.T) {
		timeout := 5 * time.Second
		plan := down(t, downState(), DownOptions{Timeout: &timeout})
		for _, name := range []string{"p-front-1", "p-api-1", "p-db-1", "p-admin-1", "p-ghost-1"} {
			if got := stopTimeoutOf(t, plan, name); got == nil || *got != timeout {
				t.Errorf("%s stop timeout = %s, want %s", name, durationString(got), timeout)
			}
		}
	})
	t.Run("empty state derives containers from the file", func(t *testing.T) {
		got := stepArgs(down(t, State{}, DownOptions{}), "RemoveContainers")
		if want := []string{"p-front-1", "p-api-1", "p-db-1,p-db-2", "p-admin-1"}; !slices.Equal(got, want) {
			t.Errorf("RemoveContainers = %v, want %v", got, want)
		}
	})
	t.Run("empty state removes the file's non-external networks", func(t *testing.T) {
		got := stepArgs(down(t, State{}, DownOptions{}), "RemoveNetwork")
		if want := []string{"p_default", "p_web"}; !slices.Equal(got, want) {
			t.Errorf("RemoveNetwork = %v, want %v", got, want)
		}
	})
}

func TestG1LiveStateDown(t *testing.T) {
	const yaml = `{services: {web: {image: nginx}}}`
	tests := []struct {
		name  string
		state State
		opts  DownOptions
		want  []string
	}{
		{"live state without containers or networks plans nothing",
			State{Live: true, Images: []string{"docker.io/library/nginx:latest"}},
			DownOptions{},
			[]string{}},
		{"live state removes the networks it lists even when the file no longer declares them",
			State{Live: true, Networks: []string{"p_old"}},
			DownOptions{},
			[]string{"RemoveNetwork p_old"}},
		{"live state removes the volumes it lists even when the file no longer declares them",
			State{Live: true, Volumes: []string{"p_old"}},
			DownOptions{Volumes: true},
			[]string{"RemoveVolume p_old"}},
		{"zero state derives containers and networks from the file",
			State{},
			DownOptions{},
			[]string{"StopContainers p-web-1", "RemoveContainers p-web-1", "RemoveNetwork p_default"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := mustDown(t, loadProject(t, yaml), tt.state, tt.opts)
			if got := describePlan(plan); !slices.Equal(got, tt.want) {
				t.Errorf("steps = %v, want %v", got, tt.want)
			}
		})
	}
}

package plan

import (
	"slices"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

func TestAC3Order(t *testing.T) {
	runOrder := func(t *testing.T, yaml string) []string {
		t.Helper()
		plan, _ := mustUp(t, loadProject(t, yaml), State{}, healthCaps, UpOptions{})
		return stepArgs(plan, "RunContainer")
	}
	t.Run("dependencies run before dependents", func(t *testing.T) {
		got := runOrder(t, `{services: {a: {image: alpine, depends_on: [b]}, b: {image: alpine, depends_on: [c]}, c: {image: alpine}}}`)
		if want := []string{"p-c-1", "p-b-1", "p-a-1"}; !slices.Equal(got, want) {
			t.Errorf("RunContainer order = %v, want %v", got, want)
		}
	})
	t.Run("independent services run by name", func(t *testing.T) {
		got := runOrder(t, `{services: {z: {image: alpine}, x: {image: alpine}, y: {image: alpine}}}`)
		if want := []string{"p-x-1", "p-y-1", "p-z-1"}; !slices.Equal(got, want) {
			t.Errorf("RunContainer order = %v, want %v", got, want)
		}
	})
	t.Run("smallest ready service goes first", func(t *testing.T) {
		got := runOrder(t, `{services: {a: {image: alpine, depends_on: [c]}, b: {image: alpine}, c: {image: alpine}}}`)
		if want := []string{"p-b-1", "p-c-1", "p-a-1"}; !slices.Equal(got, want) {
			t.Errorf("RunContainer order = %v, want %v", got, want)
		}
	})
	t.Run("dependency cycle is an error", func(t *testing.T) {
		p := loadProject(t, `{services: {a: {image: alpine, depends_on: [b]}, b: {image: alpine}}}`)
		editService(p, "b", func(s *types.ServiceConfig) {
			s.DependsOn = types.DependsOnConfig{"a": {Condition: types.ServiceConditionStarted, Required: true}}
		})
		if _, _, err := Up(p, State{}, healthCaps, UpOptions{}); err == nil {
			t.Error("Up succeeded, want an error for the a -> b -> a cycle")
		}
	})

	const createsYAML = `
services:
  web:
    build: .
    networks: [nb, na]
    volumes: ["vb:/b", "va:/a"]
networks:
  na: {}
  nb: {}
volumes:
  va: {}
  vb: {}
`
	t.Run("networks then volumes, each by engine name", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, createsYAML), State{}, healthCaps, UpOptions{})
		var got []string
		for _, d := range describePlan(plan) {
			if kind, _, _ := strings.Cut(d, " "); kind == "CreateNetwork" || kind == "CreateVolume" {
				got = append(got, d)
			}
		}
		want := []string{"CreateNetwork p_na", "CreateNetwork p_nb", "CreateVolume p_va", "CreateVolume p_vb"}
		if !slices.Equal(got, want) {
			t.Errorf("create steps = %v, want %v", got, want)
		}
	})
	t.Run("creates precede the first build or run", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, createsYAML), State{}, healthCaps, UpOptions{})
		lastCreate, firstWork := -1, -1
		for i, d := range describePlan(plan) {
			switch kind, _, _ := strings.Cut(d, " "); kind {
			case "CreateNetwork", "CreateVolume":
				lastCreate = i
			case "BuildImage", "RunContainer":
				if firstWork < 0 {
					firstWork = i
				}
			}
		}
		if lastCreate < 0 || firstWork < 0 || lastCreate > firstWork {
			t.Errorf("steps = %v, want every CreateNetwork/CreateVolume before the first BuildImage/RunContainer", describePlan(plan))
		}
	})
	t.Run("build precedes the service's first run", func(t *testing.T) {
		plan, _ := mustUp(t, loadProject(t, createsYAML), State{}, healthCaps, UpOptions{})
		build, run := stepIndex(plan, "BuildImage p-web"), stepIndex(plan, "RunContainer p-web-1")
		if build < 0 || run < 0 || build > run {
			t.Errorf("steps = %v, want BuildImage p-web before RunContainer p-web-1", describePlan(plan))
		}
	})
}

package plan

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
)

var healthCaps = Capabilities{Healthcheck: true}

// loadProject loads yaml as project "p" in /tmp/proj without reading files or the process environment.
func loadProject(t *testing.T, yaml string) *types.Project {
	t.Helper()
	p, err := loader.LoadWithContext(context.Background(), types.ConfigDetails{
		WorkingDir:  "/tmp/proj",
		ConfigFiles: []types.ConfigFile{{Filename: "compose.yaml", Content: []byte(yaml)}},
		Environment: types.Mapping{},
	}, func(o *loader.Options) { o.SetProjectName("p", true) })
	if err != nil {
		t.Fatalf("loading compose file: %v", err)
	}
	return p
}

// editService changes a loaded service, typically into a shape the loader itself rejects.
func editService(p *types.Project, name string, edit func(*types.ServiceConfig)) {
	s := p.Services[name]
	edit(&s)
	p.Services[name] = s
}

func mustUp(t *testing.T, p *types.Project, s State, caps Capabilities, opts UpOptions) (*Plan, []Warning) {
	t.Helper()
	plan, ws, err := Up(p, s, caps, opts)
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if plan == nil {
		t.Fatal("Up returned a nil Plan")
	}
	return plan, ws
}

func mustDown(t *testing.T, p *types.Project, s State, opts DownOptions) *Plan {
	t.Helper()
	plan, _, err := Down(p, s, opts)
	if err != nil {
		t.Fatalf("Down: %v", err)
	}
	if plan == nil {
		t.Fatal("Down returned a nil Plan")
	}
	return plan
}

func stepsOf[T Step](p *Plan) []T {
	var out []T
	for _, s := range p.Steps {
		if v, ok := s.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// describeStep renders the kind and identifying fields of a step, e.g. "RunContainer p-web-1".
func describeStep(s Step) string {
	switch s := s.(type) {
	case CreateNetwork:
		return "CreateNetwork " + s.Network.Name
	case CreateVolume:
		return "CreateVolume " + s.Volume.Name
	case BuildImage:
		return "BuildImage " + s.Build.Tag
	case RunContainer:
		return "RunContainer " + s.Container.Name
	case StartContainers:
		return "StartContainers " + strings.Join(s.Names, ",")
	case StopContainers:
		return "StopContainers " + strings.Join(s.Names, ",")
	case RemoveContainers:
		return "RemoveContainers " + strings.Join(s.Names, ",")
	case RemoveNetwork:
		return "RemoveNetwork " + s.Name
	case RemoveVolume:
		return "RemoveVolume " + s.Name
	case WaitContainer:
		return "WaitContainer " + s.Name + " " + string(s.Condition)
	default:
		return fmt.Sprintf("%T", s)
	}
}

func describePlan(p *Plan) []string {
	out := []string{}
	for _, s := range p.Steps {
		out = append(out, describeStep(s))
	}
	return out
}

// stepArgs returns, in plan order, what describeStep renders after the kind for each step of that kind.
func stepArgs(p *Plan, kind string) []string {
	var out []string
	for _, d := range describePlan(p) {
		if rest, ok := strings.CutPrefix(d, kind+" "); ok {
			out = append(out, rest)
		}
	}
	return out
}

func stepIndex(p *Plan, step string) int {
	return slices.Index(describePlan(p), step)
}

func sortedCopy(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

func runSpecOf(t *testing.T, p *Plan, name string) ContainerSpec {
	t.Helper()
	for _, r := range stepsOf[RunContainer](p) {
		if r.Container.Name == name {
			return r.Container
		}
	}
	t.Fatalf("no RunContainer %s in %v", name, describePlan(p))
	return ContainerSpec{}
}

func composeLabelsFor(project, service, number string) map[string]string {
	return map[string]string{LabelProject: project, LabelService: service, LabelContainerNumber: number}
}

func stateContainer(service string, n int, status string) ContainerState {
	return ContainerState{
		Name:    ContainerName("p", service, n),
		Service: service,
		Number:  n,
		Status:  status,
		Labels:  composeLabelsFor("p", service, strconv.Itoa(n)),
	}
}

func expectWarning(t *testing.T, ws []Warning, scope, attribute, prefix string) {
	t.Helper()
	var got []Warning
	for _, w := range ws {
		if w.Scope == scope && w.Attribute == attribute {
			got = append(got, w)
		}
	}
	if len(got) != 1 || !strings.HasPrefix(got[0].Message, prefix) {
		t.Errorf("warnings for %s: %s = %v, want exactly one starting with %q (all warnings: %v)", scope, attribute, got, prefix, ws)
	}
}

func kvsToMap(t *testing.T, kvs []KV) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, kv := range kvs {
		if _, dup := m[kv.Key]; dup {
			t.Errorf("key %q appears twice in %v", kv.Key, kvs)
		}
		m[kv.Key] = kv.Value
	}
	return m
}

func ptrTo[T any](v T) *T { return &v }

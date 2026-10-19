package enginetest_test

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var _ engine.Engine = (*enginetest.Fake)(nil)

// stepTypes is the fixture order Steps() must produce.
var stepTypes = []string{
	"CreateNetwork", "CreateVolume", "BuildImage", "RunContainer",
	"StartContainers", "StopContainers", "RemoveContainers",
	"RemoveNetwork", "RemoveVolume", "WaitContainer",
	"ListContainers", "ListNetworks", "ListVolumes", "ListImages",
	"InspectContainer", "Logs",
}

var parseMethods = []string{"ParseContainers", "ParseNetworks", "ParseVolumes", "ParseImages", "ParseInspect"}

type renderFn = func(plan.Step) ([]string, []plan.Warning, error)

// unknownStep is a Step type no engine knows about.
type unknownStep struct{ plan.Step }

// recordingTB captures Contract's Errorf calls instead of failing the test.
type recordingTB struct {
	testing.TB
	errs []string
}

func (*recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func typeName(s plan.Step) string {
	if s == nil {
		return "<nil>"
	}
	return reflect.TypeOf(s).Name()
}

func validJSON(method string) []byte {
	if method == "ParseInspect" {
		return []byte("{}")
	}
	return []byte("[]")
}

// parse calls the named Parse method and reports how many items it returned.
func parse(e engine.Engine, method string, in []byte) (n int, err error) {
	switch method {
	case "ParseContainers":
		var v []plan.ContainerState
		v, err = e.ParseContainers(in)
		n = len(v)
	case "ParseNetworks":
		var v []string
		v, err = e.ParseNetworks(in)
		n = len(v)
	case "ParseVolumes":
		var v []string
		v, err = e.ParseVolumes(in)
		n = len(v)
	case "ParseImages":
		var v []string
		v, err = e.ParseImages(in)
		n = len(v)
	case "ParseInspect":
		_, err = e.ParseInspect(in)
	default:
		panic("unknown parse method " + method)
	}
	return n, err
}

func wantParsed[T any](t *testing.T, method string, got T, err error, want T) {
	t.Helper()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("%s() = %v, %v; want %v, nil", method, got, err, want)
	}
}

func TestContract_AC9(t *testing.T) {
	enginetest.Contract(t, enginetest.NewFake("fake"))

	f := enginetest.NewFake("fake")
	rec := &recordingTB{TB: t}
	enginetest.Contract(rec, f)
	if len(rec.errs) != 0 {
		t.Errorf("Contract reported %d error(s) for a conforming Fake:\n%s", len(rec.errs), strings.Join(rec.errs, "\n"))
	}
	for _, want := range stepTypes {
		if !slices.ContainsFunc(f.Rendered, func(s plan.Step) bool { return typeName(s) == want }) {
			t.Errorf("Contract never rendered a %s step", want)
		}
	}
	for _, m := range parseMethods {
		if len(f.ParsedInputs[m]) == 0 {
			t.Errorf("Contract never called %s", m)
		}
	}
}

func TestContractViolations_AC10(t *testing.T) {
	period := plan.Warning{Scope: "services.web", Attribute: "restart", Message: "ignored (no restart support)."}
	isRenderStep := func(msg string) bool {
		for _, name := range stepTypes {
			if strings.HasPrefix(msg, "Render/"+name+":") {
				return true
			}
		}
		return false
	}
	tests := []struct {
		name   string
		render func(f *enginetest.Fake) renderFn
		want   string
		match  func(msg string) bool // nil means prefix match on want
	}{
		{
			name: "non-deterministic argv",
			render: func(*enginetest.Fake) renderFn {
				n := 0
				return func(plan.Step) ([]string, []plan.Warning, error) {
					n++
					return []string{"x", strconv.Itoa(n)}, nil, nil
				}
			},
			want: "Render/determinism:",
		},
		{
			name: "unknown step accepted",
			render: func(*enginetest.Fake) renderFn {
				return func(plan.Step) ([]string, []plan.Warning, error) { return []string{"x"}, nil, nil }
			},
			want: "Render/unknown-step:",
		},
		{
			name: "warning with period",
			render: func(*enginetest.Fake) renderFn {
				return func(plan.Step) ([]string, []plan.Warning, error) {
					return []string{"x"}, []plan.Warning{period}, nil
				}
			},
			want: "Warnings:",
		},
		{
			name: "argv starts with binary",
			render: func(f *enginetest.Fake) renderFn {
				return func(plan.Step) ([]string, []plan.Warning, error) { return []string{f.Binary(), "run"}, nil, nil }
			},
			want:  "Render/<Step>:",
			match: isRenderStep,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := enginetest.NewFake("fake")
			f.RenderFn = tt.render(f)
			rec := &recordingTB{TB: t}
			enginetest.Contract(rec, f)
			match := tt.match
			if match == nil {
				match = func(msg string) bool { return strings.HasPrefix(msg, tt.want) }
			}
			if !slices.ContainsFunc(rec.errs, match) {
				t.Errorf("Contract errors = %q, want one starting with %q", rec.errs, tt.want)
			}
		})
	}
}

func TestFake_AC11(t *testing.T) {
	t.Run("identity", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		if f.Name() != "fake" || f.Binary() != "fake" {
			t.Errorf("NewFake(fake): Name() = %q, Binary() = %q, want fake, fake", f.Name(), f.Binary())
		}
		if !f.Capabilities().Healthcheck {
			t.Errorf("NewFake(fake): Capabilities() = %+v, want Healthcheck true", f.Capabilities())
		}
		f.BinaryV = "/opt/fake"
		f.Caps = plan.Capabilities{}
		if f.Binary() != "/opt/fake" || f.Capabilities() != (plan.Capabilities{}) {
			t.Errorf("Binary() = %q, Capabilities() = %+v, want the assigned fields", f.Binary(), f.Capabilities())
		}
	})

	t.Run("RenderFn passthrough", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		wantArgv := []string{"a", "b"}
		wantWs := []plan.Warning{{Scope: "services.web", Attribute: "restart", Message: "ignored (unsupported)"}}
		wantErr := errors.New("boom")
		f.RenderFn = func(plan.Step) ([]string, []plan.Warning, error) { return wantArgv, wantWs, wantErr }
		argv, ws, err := f.Render(plan.ListImages{})
		if !reflect.DeepEqual(argv, wantArgv) || !reflect.DeepEqual(ws, wantWs) || err != wantErr {
			t.Errorf("Render() = %v, %v, %v; want %v, %v, %v", argv, ws, err, wantArgv, wantWs, wantErr)
		}
	})

	t.Run("Rendered records every step in order", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		f.RenderFn = func(s plan.Step) ([]string, []plan.Warning, error) {
			if n := len(f.Rendered); n == 0 || !reflect.DeepEqual(f.Rendered[n-1], s) {
				t.Errorf("RenderFn(%v) called before the step was appended to Rendered %v", s, f.Rendered)
			}
			return nil, nil, nil
		}
		steps := []plan.Step{plan.CreateNetwork{Network: plan.NetworkSpec{Name: "n"}}, plan.RemoveVolume{Name: "v"}, nil, unknownStep{}}
		for _, s := range steps {
			f.Render(s)
		}
		if !reflect.DeepEqual(f.Rendered, steps) {
			t.Errorf("Rendered = %v, want %v", f.Rendered, steps)
		}
	})

	t.Run("default RenderFn", func(t *testing.T) {
		tests := []struct {
			name        string
			step        plan.Step
			healthcheck bool
			wantErr     bool
		}{
			{"wait completed", plan.WaitContainer{Name: "c", Condition: plan.ConditionCompleted}, true, false},
			{"wait completed without healthcheck", plan.WaitContainer{Name: "c", Condition: plan.ConditionCompleted}, false, false},
			{"wait healthy", plan.WaitContainer{Name: "c", Condition: plan.ConditionHealthy}, true, false},
			{"wait healthy without healthcheck", plan.WaitContainer{Name: "c", Condition: plan.ConditionHealthy}, false, true},
			{"nil step", nil, true, true},
			{"unknown step", unknownStep{}, true, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := enginetest.NewFake("fake")
				f.Caps.Healthcheck = tt.healthcheck
				argv, _, err := f.Render(tt.step)
				if (err != nil) != tt.wantErr {
					t.Fatalf("Render() error = %v, wantErr %v", err, tt.wantErr)
				}
				if !tt.wantErr && len(argv) != 0 {
					t.Errorf("Render() argv = %v, want empty", argv)
				}
			})
		}
		t.Run("other steps", func(t *testing.T) {
			f := enginetest.NewFake("fake")
			steps := enginetest.Steps()
			if len(steps) == 0 {
				t.Fatal("Steps() is empty")
			}
			for _, s := range steps {
				if _, ok := s.(plan.WaitContainer); ok {
					continue
				}
				argv, ws, err := f.Render(s)
				argv2, ws2, err2 := f.Render(s)
				switch {
				case err != nil:
					t.Errorf("Render(%s) error = %v", typeName(s), err)
				case len(argv) == 0:
					t.Errorf("Render(%s) argv is empty", typeName(s))
				case len(ws) != 0:
					t.Errorf("Render(%s) warnings = %v, want none", typeName(s), ws)
				case err2 != nil || !reflect.DeepEqual(argv, argv2) || !reflect.DeepEqual(ws, ws2):
					t.Errorf("Render(%s) is not deterministic: %v vs %v", typeName(s), argv, argv2)
				}
			}
		})
	})

	t.Run("ParsedInputs", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		for _, m := range parseMethods {
			parse(f, m, []byte("not json"))
			parse(f, m, validJSON(m))
		}
		for _, m := range parseMethods {
			want := [][]byte{[]byte("not json"), validJSON(m)}
			if got := f.ParsedInputs[m]; !reflect.DeepEqual(got, want) {
				t.Errorf("ParsedInputs[%q] = %q, want %q", m, got, want)
			}
		}
	})

	t.Run("configured values", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		f.Containers = []plan.ContainerState{{Name: "p-web-1", Service: "web", Number: 1}}
		f.Networks = []string{"p_default"}
		f.Volumes = []string{"p_data"}
		f.Images = []string{"p-web"}
		cs, err := f.ParseContainers([]byte("[]"))
		wantParsed(t, "ParseContainers", cs, err, f.Containers)
		ns, err := f.ParseNetworks([]byte("[]"))
		wantParsed(t, "ParseNetworks", ns, err, f.Networks)
		vs, err := f.ParseVolumes([]byte("[]"))
		wantParsed(t, "ParseVolumes", vs, err, f.Volumes)
		is, err := f.ParseImages([]byte("[]"))
		wantParsed(t, "ParseImages", is, err, f.Images)
	})

	t.Run("Inspects", func(t *testing.T) {
		if _, err := enginetest.NewFake("fake").ParseInspect([]byte("{}")); err == nil {
			t.Error("ParseInspect() with no Inspects: error = nil, want error")
		}
		f := enginetest.NewFake("fake")
		a, b := plan.ContainerState{Name: "a"}, plan.ContainerState{Name: "b"}
		f.Inspects = []plan.ContainerState{a, b}
		for i, want := range []plan.ContainerState{a, b, b, b} {
			got, err := f.ParseInspect([]byte("{}"))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("ParseInspect() call %d = %v, %v; want %v, nil", i, got, err, want)
			}
		}
	})

	t.Run("ParseErr", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		f.Inspects = []plan.ContainerState{{Name: "a"}}
		f.ParseErr = errors.New("boom")
		for _, m := range parseMethods {
			if _, err := parse(f, m, validJSON(m)); !errors.Is(err, f.ParseErr) {
				t.Errorf("%s() error = %v, want %v", m, err, f.ParseErr)
			}
		}
	})

	t.Run("blank and invalid input", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		f.Containers = []plan.ContainerState{{Name: "a"}}
		f.Networks = []string{"n"}
		f.Volumes = []string{"v"}
		f.Images = []string{"i"}
		f.Inspects = []plan.ContainerState{{Name: "a"}}
		for _, m := range parseMethods {
			for _, in := range [][]byte{nil, []byte(""), []byte(" \n\t")} {
				n, err := parse(f, m, in)
				if m == "ParseInspect" {
					if err == nil {
						t.Errorf("%s(%q) error = nil, want error", m, in)
					}
					continue
				}
				if n != 0 || err != nil {
					t.Errorf("%s(%q) = %d items, %v; want 0, nil", m, in, n, err)
				}
			}
			if _, err := parse(f, m, []byte("not json")); err == nil {
				t.Errorf("%s(not json) error = nil, want error", m)
			}
		}
	})
}

func TestSteps_AC11(t *testing.T) {
	a, b := enginetest.Steps(), enginetest.Steps()
	if len(a) != len(stepTypes) {
		t.Fatalf("len(Steps()) = %d, want %d", len(a), len(stepTypes))
	}
	for i, s := range a {
		if got := typeName(s); got != stepTypes[i] {
			t.Errorf("Steps()[%d] is %s, want %s", i, got, stepTypes[i])
		}
		if w, ok := s.(plan.WaitContainer); ok && w.Condition != plan.ConditionCompleted {
			t.Errorf("Steps() WaitContainer.Condition = %q, want %q", w.Condition, plan.ConditionCompleted)
		}
	}
	if !reflect.DeepEqual(a, b) {
		t.Errorf("Steps() differs between calls:\n%v\n%v", a, b)
	}
	// Writing through a's slice fields must not leak into b.
	mutated := 0
	for _, s := range a {
		v := reflect.ValueOf(s)
		if v.Kind() != reflect.Struct {
			continue
		}
		for j := range v.NumField() {
			f := v.Field(j)
			if f.Kind() != reflect.Slice || f.Len() == 0 || f.Type().Elem().Kind() != reflect.String {
				continue
			}
			if e := f.Index(0); e.CanSet() {
				e.SetString("mutated")
				mutated++
			}
		}
	}
	if mutated == 0 {
		t.Error("Steps() has no non-empty []string field to prove slice independence")
	}
	if !reflect.DeepEqual(b, enginetest.Steps()) {
		t.Errorf("Steps() shares slice storage between calls: %v", b)
	}
}

// tstScopedWarnings renders like the default Fake but attaches one warning with the given scope to every step that yields argv.
func tstScopedWarnings(scope string) renderFn {
	base := enginetest.NewFake("fake")
	return func(s plan.Step) ([]string, []plan.Warning, error) {
		argv, ws, err := base.Render(s)
		if err != nil || len(argv) == 0 {
			return argv, ws, err
		}
		return argv, []plan.Warning{{Scope: scope, Attribute: "volumes", Message: "ignored (no -v)"}}, nil
	}
}

func TestS5ContainersScope(t *testing.T) {
	t.Run("containers. scope conforms", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		f.RenderFn = tstScopedWarnings("containers.x")
		rec := &recordingTB{TB: t}
		enginetest.Contract(rec, f)
		if len(rec.errs) != 0 {
			t.Errorf("Contract reported %d error(s) for a containers. warning scope:\n%s", len(rec.errs), strings.Join(rec.errs, "\n"))
		}
	})
	t.Run("foo. scope is rejected", func(t *testing.T) {
		f := enginetest.NewFake("fake")
		f.RenderFn = tstScopedWarnings("foo.x")
		rec := &recordingTB{TB: t}
		enginetest.Contract(rec, f)
		if !slices.ContainsFunc(rec.errs, func(msg string) bool { return strings.HasPrefix(msg, "Warnings:") }) {
			t.Errorf("Contract errors = %q, want one starting with %q", rec.errs, "Warnings:")
		}
	})
}

func TestS5StepsFixture(t *testing.T) {
	var builds, runs int
	for _, s := range enginetest.Steps() {
		switch s := s.(type) {
		case plan.BuildImage:
			builds++
			if s.Build.Service != "web" {
				t.Errorf("Steps() BuildImage.Build.Service = %q, want %q", s.Build.Service, "web")
			}
		case plan.RunContainer:
			runs++
			if len(s.Container.Networks) == 0 {
				t.Error("Steps() RunContainer has no network attachments")
			}
			for i, n := range s.Container.Networks {
				if n.Key == "" {
					t.Errorf("Steps() RunContainer.Container.Networks[%d] (%s) has an empty Key", i, n.Name)
				}
			}
		}
	}
	if builds == 0 || runs == 0 {
		t.Errorf("Steps() has %d BuildImage and %d RunContainer steps, want at least one of each", builds, runs)
	}
}

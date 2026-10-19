package dockerlike_test

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/dockerlike"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// tstVariants lists the docker-style dialects in the order Variants() must report them.
var tstVariants = []string{"docker", "podman", "nerdctl", "wslc"}

func tstNew(t *testing.T, variant string) engine.Engine {
	t.Helper()
	e, err := dockerlike.New(variant)
	if err != nil {
		t.Fatalf("New(%q) error: %v", variant, err)
	}
	if e == nil {
		t.Fatalf("New(%q) = nil engine", variant)
	}
	return e
}

func TestContract(t *testing.T) {
	for _, v := range tstVariants {
		t.Run(v, func(t *testing.T) {
			enginetest.Contract(t, tstNew(t, v))
		})
	}
}

func TestNew(t *testing.T) {
	for _, v := range tstVariants {
		t.Run(v, func(t *testing.T) {
			e := tstNew(t, v)
			if e.Name() != v {
				t.Errorf("Name() = %q, want %q", e.Name(), v)
			}
			if e.Binary() != v {
				t.Errorf("Binary() = %q, want %q", e.Binary(), v)
			}
			if got, want := e.Capabilities(), (plan.Capabilities{Healthcheck: true}); got != want {
				t.Errorf("Capabilities() = %+v, want %+v", got, want)
			}
		})
	}

	unknown := []struct{ name, variant string }{
		{"empty", ""},
		{"trailing space", "docker "},
		{"leading space", " docker"},
		{"upper case", "Docker"},
		{"finch", "finch"},
		{"container", "container"},
	}
	for _, tt := range unknown {
		t.Run("unknown "+tt.name, func(t *testing.T) {
			e, err := dockerlike.New(tt.variant)
			if !errors.Is(err, engine.ErrUnknownEngine) {
				t.Errorf("New(%q) error = %v, want errors.Is(err, engine.ErrUnknownEngine)", tt.variant, err)
			}
			if e != nil {
				t.Errorf("New(%q) = %v, want nil engine", tt.variant, e)
			}
		})
	}
}

func TestVariants(t *testing.T) {
	got := dockerlike.Variants()
	if !reflect.DeepEqual(got, tstVariants) {
		t.Fatalf("Variants() = %q, want %q", got, tstVariants)
	}
	probed := slices.Concat(engine.Order("linux"), engine.Order("windows"))
	for _, v := range got {
		if !slices.Contains(probed, v) {
			t.Errorf("Variants() includes %q, which engine.Order never probes on linux or windows", v)
		}
	}
}

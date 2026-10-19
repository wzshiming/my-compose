package engine_test

import (
	"errors"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
)

var allNames = []string{"container", "docker", "podman", "nerdctl", "wslc"}

func fakes(names ...string) []engine.Engine {
	out := make([]engine.Engine, 0, len(names))
	for _, n := range names {
		out = append(out, enginetest.NewFake(n))
	}
	return out
}

// found returns a lookPath verdict that finds only the named binaries.
func found(names ...string) func(string) error {
	return func(bin string) error {
		if slices.Contains(names, bin) {
			return nil
		}
		return exec.ErrNotFound
	}
}

func TestOrder_AC1(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want []string
	}{
		{"darwin", "darwin", []string{"container", "docker", "podman", "nerdctl"}},
		{"windows", "windows", []string{"wslc", "docker", "podman", "nerdctl"}},
		{"linux", "linux", []string{"docker", "podman", "nerdctl"}},
		{"empty", "", []string{"docker", "podman", "nerdctl"}},
		{"plan9", "plan9", []string{"docker", "podman", "nerdctl"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := engine.Order(tt.goos); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Order(%q) = %v, want %v", tt.goos, got, tt.want)
			}
		})
	}
	t.Run("fresh slice", func(t *testing.T) {
		a, b := engine.Order("darwin"), engine.Order("darwin")
		if len(a) == 0 || !reflect.DeepEqual(a, b) {
			t.Fatalf("Order(darwin) = %v and %v, want equal non-empty slices", a, b)
		}
		a[0] = "mutated"
		if b[0] == "mutated" {
			t.Errorf("Order() shares its slice between calls: %v", b)
		}
	})
}

func TestResolve(t *testing.T) {
	denied := errors.New("permission denied")
	tests := []struct {
		name       string
		explicit   string
		goos       string
		look       func(string) error
		candidates []string
		wantName   string
		wantErr    error
		wantCalls  []string // nil skips the check
		wantMsg    []string
	}{
		{
			name:       "AC2 explicit match skips lookPath",
			explicit:   "wslc",
			goos:       "darwin",
			look:       found(),
			candidates: allNames,
			wantName:   "wslc",
			wantCalls:  []string{},
		},
		{
			name:       "AC3 explicit is trimmed",
			explicit:   " docker ",
			goos:       "darwin",
			look:       found(),
			candidates: allNames,
			wantName:   "docker",
			wantCalls:  []string{},
		},
		{
			name:       "AC3 explicit is case-sensitive",
			explicit:   "Docker",
			goos:       "darwin",
			look:       found(allNames...),
			candidates: allNames,
			wantErr:    engine.ErrUnknownEngine,
			wantMsg:    append([]string{`unknown engine "Docker"`}, allNames...),
		},
		{
			name:       "AC4 darwin probes in order until podman",
			goos:       "darwin",
			look:       found("podman"),
			candidates: allNames,
			wantName:   "podman",
			wantCalls:  []string{"container", "docker", "podman"},
		},
		{
			name:       "AC5 linux prefers docker",
			goos:       "linux",
			look:       found(allNames...),
			candidates: allNames,
			wantName:   "docker",
		},
		{
			name:       "AC5 linux never probes container",
			goos:       "linux",
			look:       found("container"),
			candidates: allNames,
			wantErr:    engine.ErrNoEngine,
		},
		{
			name:       "AC6 windows without wslc candidate",
			goos:       "windows",
			look:       found(),
			candidates: []string{"container", "docker", "podman", "nerdctl"},
			wantErr:    engine.ErrNoEngine,
			wantMsg:    []string{"tried: docker, podman, nerdctl", "--engine", "CONTAINER_COMPOSE_ENGINE"},
		},
		{
			name: "AC7 any lookPath error skips the engine",
			goos: "linux",
			look: func(bin string) error {
				switch bin {
				case "docker":
					return denied
				case "podman":
					return nil
				}
				return exec.ErrNotFound
			},
			candidates: allNames,
			wantName:   "podman",
			wantCalls:  []string{"docker", "podman"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := []string{}
			lookPath := func(bin string) (string, error) {
				calls = append(calls, bin)
				if err := tt.look(bin); err != nil {
					return "", err
				}
				return "/usr/local/bin/" + bin, nil
			}
			got, err := engine.Resolve(tt.explicit, tt.goos, lookPath, fakes(tt.candidates...))
			if tt.wantCalls != nil && !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Errorf("lookPath calls = %v, want %v", calls, tt.wantCalls)
			}
			if tt.wantName != "" {
				if err != nil {
					t.Fatalf("Resolve() error = %v, want nil", err)
				}
				if got == nil || got.Name() != tt.wantName {
					t.Fatalf("Resolve() = %v, want engine %q", got, tt.wantName)
				}
				return
			}
			// AC8: failures return a nil Engine and wrap exactly one sentinel.
			if got != nil {
				t.Errorf("Resolve() = %v, want nil engine on error", got)
			}
			if err == nil {
				t.Fatalf("Resolve() error = nil, want %v", tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Resolve() error = %v, want errors.Is %v", err, tt.wantErr)
			}
			for _, other := range []error{engine.ErrNoEngine, engine.ErrUnknownEngine} {
				if other != tt.wantErr && errors.Is(err, other) {
					t.Errorf("Resolve() error = %v, must not match %v", err, other)
				}
			}
			for _, s := range tt.wantMsg {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("Resolve() error = %q, want it to contain %q", err, s)
				}
			}
		})
	}
}

func TestSentinels_AC8(t *testing.T) {
	if engine.ErrNoEngine == nil || engine.ErrUnknownEngine == nil {
		t.Fatal("ErrNoEngine and ErrUnknownEngine must be non-nil sentinels")
	}
	if errors.Is(engine.ErrNoEngine, engine.ErrUnknownEngine) || errors.Is(engine.ErrUnknownEngine, engine.ErrNoEngine) {
		t.Error("ErrNoEngine and ErrUnknownEngine must be distinct")
	}
}

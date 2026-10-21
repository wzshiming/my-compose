package dockercli_test

import (
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

func TestContract(t *testing.T) {
	for _, cfg := range tstConfigs() {
		t.Run(cfg.label, func(t *testing.T) {
			enginetest.Contract(t, dockercli.New(cfg.v))
		})
	}
}

func TestNewDelegates(t *testing.T) {
	other := tstFull()
	other.name, other.binary, other.healthcheck = "other", "other-cli", false
	for _, tt := range []struct {
		label string
		v     tstVariant
	}{{"full", tstFull()}, {"other", other}} {
		t.Run(tt.label, func(t *testing.T) {
			e := dockercli.New(tt.v)
			if e == nil {
				t.Fatal("New() = nil engine")
			}
			if e.Name() != tt.v.name {
				t.Errorf("Name() = %q, want %q", e.Name(), tt.v.name)
			}
			if e.Binary() != tt.v.binary {
				t.Errorf("Binary() = %q, want %q", e.Binary(), tt.v.binary)
			}
			if got, want := e.Capabilities(), (plan.Capabilities{Healthcheck: tt.v.healthcheck}); got != want {
				t.Errorf("Capabilities() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestWaitHealthy(t *testing.T) {
	step := plan.WaitContainer{Name: "proj-web-1", Condition: plan.ConditionHealthy}
	t.Run("without healthcheck support is an error", func(t *testing.T) {
		argv, warnings, err := dockercli.New(tstNoHealth()).Render(step)
		if want := "stub cannot wait for proj-web-1 to be healthy without healthcheck support"; err == nil || err.Error() != want {
			t.Errorf("Render(WaitContainer healthy) error = %v, want %q", err, want)
		}
		if argv != nil || warnings != nil {
			t.Errorf("Render(WaitContainer healthy) = %q, %q; want nil argv and nil warnings", argv, warnings)
		}
	})
	t.Run("with healthcheck support renders nothing", func(t *testing.T) {
		argv, warnings, err := dockercli.New(tstFull()).Render(step)
		if argv != nil || warnings != nil || err != nil {
			t.Errorf("Render(WaitContainer healthy) = %q, %q, %v; want nil, nil, nil", argv, warnings, err)
		}
	})
}

func TestWaitCompleted(t *testing.T) {
	step := plan.WaitContainer{Name: "proj-web-1", Condition: plan.ConditionCompleted}
	for _, cfg := range []tstConfig{{"full", tstFull()}, {"noHealth", tstNoHealth()}} {
		t.Run(cfg.label, func(t *testing.T) {
			argv, warnings, err := dockercli.New(cfg.v).Render(step)
			if argv != nil || warnings != nil || err != nil {
				t.Errorf("Render(WaitContainer completed) = %q, %q, %v; want nil, nil, nil", argv, warnings, err)
			}
		})
	}
}

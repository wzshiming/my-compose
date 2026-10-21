package nerdctl_test

import (
	"flag"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/engine/nerdctl"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var tstUpdate = flag.Bool("update", false, "rewrite testdata/steps.txt from the current Render output")

func TestIdentity(t *testing.T) {
	e := nerdctl.New()
	if got := e.Name(); got != "nerdctl" {
		t.Errorf("Name() = %q, want %q", got, "nerdctl")
	}
	if got := e.Binary(); got != "nerdctl" {
		t.Errorf("Binary() = %q, want %q", got, "nerdctl")
	}
	if got, want := e.Capabilities(), (plan.Capabilities{Healthcheck: true}); got != want {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
	if order := engine.Order("linux"); !slices.Contains(order, e.Name()) {
		t.Errorf("Order(linux) = %q lacks %q", order, e.Name())
	}
	// The flag table, observed through Render: run takes --restart but not --network-alias.
	tstRender(t, []tstRenderCase{
		{
			name: "has run --restart",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Restart = "always" }),
			argv: tstRunArgv([]string{"--restart", "always"}),
		},
		{
			name: "lacks run --network-alias",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"a"}}}
			}),
			argv:  tstRunArgv([]string{"--network", "proj_front"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.front.aliases")},
		},
	})
}

func TestContract(t *testing.T) {
	enginetest.Contract(t, nerdctl.New())
}

func TestGolden(t *testing.T) {
	enginetest.Golden(t, filepath.Join("testdata", "steps.txt"), enginetest.RenderSteps(nerdctl.New()), *tstUpdate)
}

package podman_test

import (
	"flag"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/engine/podman"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var tstUpdate = flag.Bool("update", false, "rewrite testdata/steps.txt from the current Render output")

func TestIdentity(t *testing.T) {
	e := podman.New()
	if got := e.Name(); got != "podman" {
		t.Errorf("Name() = %q, want %q", got, "podman")
	}
	if got := e.Binary(); got != "podman" {
		t.Errorf("Binary() = %q, want %q", got, "podman")
	}
	if got, want := e.Capabilities(), (plan.Capabilities{Healthcheck: true}); got != want {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
	if order := engine.Order("linux"); !slices.Contains(order, e.Name()) {
		t.Errorf("Order(linux) = %q does not probe %q", order, e.Name())
	}
	// The flag table is only reachable through Render: a missing flag is dropped with a reason naming podman, a present one renders.
	tstRender(t, []tstRenderCase{
		{
			name:  "has no run --link",
			step:  tstRunStep(func(c *plan.ContainerSpec) { c.Links = []string{"proj-db-1:db"} }),
			argv:  tstRunArgv(nil),
			warns: []tstWarn{{Scope: tstWeb, Attribute: "links", Prefix: "ignored (podman has no --link)"}},
		},
		{
			name: "has run --restart",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Restart = "always" }),
			argv: tstRunArgv([]string{"--restart", "always"}),
		},
	})
}

func TestContract(t *testing.T) {
	enginetest.Contract(t, podman.New())
}

func TestGolden(t *testing.T) {
	enginetest.Golden(t, filepath.Join("testdata", "steps.txt"), enginetest.RenderSteps(podman.New()), *tstUpdate)
}

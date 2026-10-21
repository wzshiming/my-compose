package wslc_test

import (
	"flag"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/engine/wslc"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var tstUpdate = flag.Bool("update", false, "rewrite testdata/steps.txt from the current Render output")

func TestIdentity(t *testing.T) {
	e := wslc.New()
	if got := e.Name(); got != "wslc" {
		t.Errorf("Name() = %q, want %q", got, "wslc")
	}
	if got := e.Binary(); got != "wslc" {
		t.Errorf("Binary() = %q, want %q", got, "wslc")
	}
	if got, want := e.Capabilities(), (plan.Capabilities{Healthcheck: true}); got != want {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
	if order := engine.Order("windows"); !slices.Contains(order, e.Name()) {
		t.Errorf("engine.Order(\"windows\") = %q, which never probes %q", order, e.Name())
	}
	// The flag table is reachable only through Render: wslc lacks run --restart but has run --label.
	argv, _, err := e.Render(tstRunStep(func(c *plan.ContainerSpec) {
		c.Restart = "always"
		c.Labels = []plan.KV{{Key: plan.LabelProject, Value: "proj"}}
	}))
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	if slices.Contains(argv, "--restart") {
		t.Errorf("argv = %q, has --restart which wslc lacks", argv)
	}
	if !slices.Contains(argv, "--label") {
		t.Errorf("argv = %q, lacks --label which wslc has", argv)
	}
}

func TestContract(t *testing.T) {
	enginetest.Contract(t, wslc.New())
}

func TestGolden(t *testing.T) {
	enginetest.Golden(t, filepath.Join("testdata", "steps.txt"), enginetest.RenderSteps(wslc.New()), *tstUpdate)
}

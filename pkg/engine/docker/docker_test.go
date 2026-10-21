package docker_test

import (
	"flag"
	"path/filepath"
	"slices"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/docker"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var tstUpdate = flag.Bool("update", false, "rewrite testdata/steps.txt from the current Render output")

func TestIdentity(t *testing.T) {
	e := docker.New()
	if got := e.Name(); got != "docker" {
		t.Errorf("Name() = %q, want %q", got, "docker")
	}
	if got := e.Binary(); got != "docker" {
		t.Errorf("Binary() = %q, want %q", got, "docker")
	}
	if got, want := e.Capabilities(), (plan.Capabilities{Healthcheck: true}); got != want {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
	if order := engine.Order("linux"); !slices.Contains(order, e.Name()) {
		t.Errorf("Order(linux) = %q does not probe %q", order, e.Name())
	}
}

func TestContract(t *testing.T) {
	enginetest.Contract(t, docker.New())
}

func TestGolden(t *testing.T) {
	enginetest.Golden(t, filepath.Join("testdata", "steps.txt"), enginetest.RenderSteps(docker.New()), *tstUpdate)
}

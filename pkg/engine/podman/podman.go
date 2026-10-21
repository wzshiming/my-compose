package podman

import (
	"slices"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// New returns the podman engine.
func New() engine.Engine { return dockercli.New(variant{}) }

type variant struct{}

var _ dockercli.Variant = variant{}

// missing lists the "<subcommand> <flag>" pairs podman lacks.
var missing = []string{
	"run --link", "run --volume-driver", "run --cpu-count", "run --cpu-percent", "run --isolation",
	"run --link-local-ip", "run --health-start-interval", "run --mount volume-nocopy",
	"network create --attachable", "network create --ipv4",
	"build --allow",
}

func (variant) Name() string                    { return "podman" }
func (variant) Binary() string                  { return "podman" }
func (variant) Capabilities() plan.Capabilities { return plan.Capabilities{Healthcheck: true} }
func (variant) ListFormat() string              { return "json" }
func (variant) PSNoTrunc() bool                 { return false }
func (variant) Has(command, flag string) bool   { return !slices.Contains(missing, command+" "+flag) }
func (variant) IntegerCPUs() bool               { return false }
func (variant) Aliases() dockercli.AliasMode    { return dockercli.AliasAll }
func (variant) NetworkKeys() []string           { return nil }

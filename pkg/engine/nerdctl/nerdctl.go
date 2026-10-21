package nerdctl

import (
	"slices"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// New returns the nerdctl engine.
func New() engine.Engine { return dockercli.New(variant{}) }

type variant struct{}

var _ dockercli.Variant = variant{}

// missing lists the "<subcommand> <flag>" pairs nerdctl lacks.
var missing = []string{
	"run --link", "run --storage-opt", "run --volume-driver", "run --expose", "run --cpu-count",
	"run --cpu-percent", "run --device-cgroup-rule", "run --link-local-ip", "run --network-alias",
	"run --health-start-interval", "run --mount volume-subpath",
	"network create --attachable",
	"volume create --driver", "volume create --opt",
	"build --shm-size", "build --ulimit", "build --isolation",
}

func (variant) Name() string                    { return "nerdctl" }
func (variant) Binary() string                  { return "nerdctl" }
func (variant) Capabilities() plan.Capabilities { return plan.Capabilities{Healthcheck: true} }
func (variant) ListFormat() string              { return "{{json .}}" }
func (variant) PSNoTrunc() bool                 { return true }
func (variant) Has(command, flag string) bool   { return !slices.Contains(missing, command+" "+flag) }
func (variant) IntegerCPUs() bool               { return false }
func (variant) Aliases() dockercli.AliasMode    { return dockercli.AliasHostname }
func (variant) NetworkKeys() []string           { return nil }

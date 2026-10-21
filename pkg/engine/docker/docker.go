package docker

import (
	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// variant is the reference CLI: every flag dockercli knows is accepted.
type variant struct{}

var _ dockercli.Variant = variant{}

// New returns the docker engine.
func New() engine.Engine { return dockercli.New(variant{}) }

func (variant) Name() string                    { return "docker" }
func (variant) Binary() string                  { return "docker" }
func (variant) Capabilities() plan.Capabilities { return plan.Capabilities{Healthcheck: true} }
func (variant) ListFormat() string              { return "{{json .}}" }
func (variant) PSNoTrunc() bool                 { return true }
func (variant) Has(_, _ string) bool            { return true }
func (variant) IntegerCPUs() bool               { return false }
func (variant) Aliases() dockercli.AliasMode    { return dockercli.AliasFirst }
func (variant) NetworkKeys() []string {
	return []string{"alias", "ip", "ip6", "mac-address", "link-local-ip"}
}

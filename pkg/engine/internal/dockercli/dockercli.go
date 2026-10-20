package dockercli

import (
	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// AliasMode says where a CLI binds network aliases when a container joins several networks.
type AliasMode int

const (
	AliasFirst    AliasMode = iota // --network-alias binds to the first --network; later networks take alias= keys
	AliasAll                       // --network-alias binds to every --network, so all aliases go there
	AliasHostname                  // no --network-alias; an alias equal to the service degrades to --hostname
)

// Variant holds every difference between the CLIs; render and parse code branches only on its methods.
type Variant interface {
	Name() string
	Binary() string
	Capabilities() plan.Capabilities
	// ListFormat is the --format value of the list commands.
	ListFormat() string
	// PSNoTrunc reports whether ps -a takes --no-trunc.
	PSNoTrunc() bool
	// Has reports whether command ("run", "build", "network create" or "volume create") takes flag, named without its value ("--ipv4" for "--ipv4=false") or as a "--mount <key>" sub-key ("--mount volume-nocopy").
	Has(command, flag string) bool
	// IntegerCPUs reports whether --cpus takes whole numbers only.
	IntegerCPUs() bool
	Aliases() AliasMode
	// NetworkKeys lists the keys allowed in --network name=<net>,<k>=<v> from the second network on; nil means none.
	NetworkKeys() []string
}

// New returns the engine for v.
func New(v Variant) engine.Engine { return dialect{v: v} }

type dialect struct{ v Variant }

var _ engine.Engine = dialect{}

func (d dialect) Name() string                    { return d.v.Name() }
func (d dialect) Binary() string                  { return d.v.Binary() }
func (d dialect) Capabilities() plan.Capabilities { return d.v.Capabilities() }

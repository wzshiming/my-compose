package dockerlike

import (
	"fmt"
	"strings"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/plan"
)

type aliasMode int

const (
	aliasFirst    aliasMode = iota // --network-alias binds to the first --network; later networks take alias= keys
	aliasAll                       // --network-alias binds to every --network, so all aliases go there
	aliasHostname                  // no --network-alias; an alias equal to the service degrades to --hostname
)

// variant holds every difference between the CLIs; render and parse code branches only on these fields.
type variant struct {
	name        string
	listFormat  string
	psNoTrunc   bool
	integerCPUs bool
	aliasMode   aliasMode
	networkKV   []string // keys allowed in --network name=<net>,<k>=<v> from the second network on
	missing     []string // "<subcommand> <flag>" the CLI lacks
}

var variants = []variant{
	{
		name:       "docker",
		listFormat: "{{json .}}",
		psNoTrunc:  true,
		networkKV:  []string{"alias", "ip", "ip6", "mac-address", "link-local-ip"},
	},
	{
		name:       "podman",
		listFormat: "json",
		aliasMode:  aliasAll,
		missing: []string{
			"run --link", "run --volume-driver", "run --cpu-count", "run --cpu-percent", "run --isolation",
			"run --link-local-ip", "run --health-start-interval", "run --mount volume-nocopy",
			"network create --attachable", "network create --ipv4",
			"build --allow",
		},
	},
	{
		name:       "nerdctl",
		listFormat: "{{json .}}",
		psNoTrunc:  true,
		aliasMode:  aliasHostname,
		missing: []string{
			"run --link", "run --storage-opt", "run --volume-driver", "run --expose", "run --cpu-count",
			"run --cpu-percent", "run --device-cgroup-rule", "run --link-local-ip", "run --network-alias",
			"run --health-start-interval", "run --mount volume-subpath",
			"network create --attachable",
			"volume create --driver", "volume create --opt",
			"build --shm-size", "build --ulimit", "build --isolation",
		},
	},
	{
		name:        "wslc",
		listFormat:  "json",
		integerCPUs: true,
		networkKV:   []string{"alias"},
		missing: []string{
			"run --restart", "run --privileged", "run --cap-add", "run --cap-drop", "run --device", "run --add-host",
			"run --pid", "run --ipc", "run --uts", "run --userns", "run --read-only", "run --init", "run --sysctl",
			"run --security-opt", "run --platform", "run --expose", "run --group-add", "run --cgroup-parent",
			"run --cgroupns", "run --runtime", "run --isolation", "run --cpu-shares", "run --cpuset-cpus",
			"run --cpu-period", "run --cpu-quota", "run --cpu-rt-period", "run --cpu-rt-runtime", "run --cpu-count",
			"run --cpu-percent", "run --memory-reservation", "run --memory-swap", "run --memory-swappiness",
			"run --oom-kill-disable", "run --oom-score-adj", "run --pids-limit", "run --blkio-weight",
			"run --log-driver", "run --annotation", "run --link", "run --volumes-from", "run --volume-driver",
			"run --storage-opt", "run --mac-address", "run --ip6", "run --link-local-ip", "run --device-cgroup-rule",
			"run --health-start-interval",
			"run --mount bind-selinux", "run --mount bind-propagation", "run --mount volume-nocopy",
			"run --mount volume-subpath", "run --mount image-subpath", "run --mount tmpfs-size", "run --mount tmpfs-mode",
			"network create --attachable", "network create --ipv4", "network create --ipv6",
			"network create --ipam-driver", "network create --ipam-opt", "network create --aux-address",
			"build --platform", "build --cache-from", "build --cache-to", "build --ssh", "build --secret",
			"build --network", "build --shm-size", "build --add-host", "build --ulimit", "build --isolation",
			"build --build-context", "build --allow",
		},
	},
}

// Variants lists the CLI names New accepts.
func Variants() []string {
	names := make([]string, len(variants))
	for i, v := range variants {
		names[i] = v.name
	}
	return names
}

// New returns the engine for one of Variants.
func New(name string) (engine.Engine, error) {
	for i := range variants {
		if variants[i].name == name {
			return dialect{v: &variants[i]}, nil
		}
	}
	return nil, fmt.Errorf("dockerlike: %q is not one of %s: %w", name, strings.Join(Variants(), ", "), engine.ErrUnknownEngine)
}

type dialect struct{ v *variant }

func (d dialect) Name() string                    { return d.v.name }
func (d dialect) Binary() string                  { return d.v.name }
func (d dialect) Capabilities() plan.Capabilities { return plan.Capabilities{Healthcheck: true} }

package wslc

import (
	"slices"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

type variant struct{}

var _ dockercli.Variant = variant{}

// New returns the wslc engine.
func New() engine.Engine { return dockercli.New(variant{}) }

func (variant) Name() string                    { return "wslc" }
func (variant) Binary() string                  { return "wslc" }
func (variant) Capabilities() plan.Capabilities { return plan.Capabilities{Healthcheck: true} }
func (variant) ListFormat() string              { return "json" }
func (variant) PSNoTrunc() bool                 { return false }
func (variant) IntegerCPUs() bool               { return true }
func (variant) Aliases() dockercli.AliasMode    { return dockercli.AliasFirst }
func (variant) NetworkKeys() []string           { return []string{"alias"} }

// missing lists the "<command> <flag>" pairs the wslc CLI lacks.
var missing = []string{
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
}

func (variant) Has(command, flag string) bool { return !slices.Contains(missing, command+" "+flag) }

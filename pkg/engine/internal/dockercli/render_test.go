package dockercli_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

func TestRunMinimal(t *testing.T) {
	var cases []tstRenderCase
	for _, cfg := range []tstConfig{{"full", tstFull()}, {"bare", tstBare()}} {
		cases = append(cases,
			tstRenderCase{name: cfg.label + "/name and image only", v: cfg.v, step: tstRunStep(nil), argv: tstRunArgv(nil)},
			tstRenderCase{name: cfg.label + "/silent zero values", v: cfg.v, step: tstRunStep(func(c *plan.ContainerSpec) {
				c.PullPolicy = "missing"
				c.Init = tstPtr(false)
				c.Restart = ""
			}), argv: tstRunArgv(nil)},
		)
	}
	tstRender(t, cases)
}

func TestRunCommand(t *testing.T) {
	tstRender(t, []tstRenderCase{
		{
			name:  "entrypoint rest without command degrades",
			step:  tstRunStep(func(c *plan.ContainerSpec) { c.Entrypoint = []string{"/bin/sh", "-c"} }),
			argv:  tstRunArgv([]string{"--entrypoint", "/bin/sh"}, "-c"),
			warns: []tstWarn{tstDegraded(tstWeb, "entrypoint", "/bin/sh")},
		},
		{
			name: "entrypoint rest precedes command",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Entrypoint = []string{"/bin/sh", "-c"}
				c.Command = []string{"echo hi"}
			}),
			argv: tstRunArgv([]string{"--entrypoint", "/bin/sh"}, "-c", "echo hi"),
		},
		{
			name: "single entrypoint and command",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Entrypoint = []string{"/entry.sh"}
				c.Command = []string{"serve", "--port", "80"}
			}),
			argv: tstRunArgv([]string{"--entrypoint", "/entry.sh"}, "serve", "--port", "80"),
		},
		{
			name: "command only",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Command = []string{"nginx", "-g", "daemon off;"} }),
			argv: tstRunArgv(nil, "nginx", "-g", "daemon off;"),
		},
		{
			name: "empty entrypoint clears the image default",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Entrypoint = []string{} }),
			argv: tstRunArgv([]string{"--entrypoint", ""}),
		},
		{
			name:  "empty command is ignored",
			step:  tstRunStep(func(c *plan.ContainerSpec) { c.Command = []string{} }),
			argv:  tstRunArgv(nil),
			warns: []tstWarn{tstIgnored(tstWeb, "command")},
		},
	})
}

func TestRunBasics(t *testing.T) {
	tstRender(t, []tstRenderCase{
		{
			name: "pull always",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.PullPolicy = "always" }),
			argv: tstRunArgv([]string{"--pull", "always"}),
		},
		{
			name: "pull never",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.PullPolicy = "never" }),
			argv: tstRunArgv([]string{"--pull", "never"}),
		},
		{
			name: "pull missing is the default",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.PullPolicy = "missing" }),
			argv: tstRunArgv(nil),
		},
		{
			name: "platform",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Platform = "linux/amd64" }),
			argv: tstRunArgv([]string{"--platform", "linux/amd64"}),
		},
		{
			name: "labels then annotations",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Labels = []plan.KV{{Key: plan.LabelProject, Value: "proj"}, {Key: plan.LabelService, Value: "web"}}
				c.Annotations = []plan.KV{{Key: "com.example.owner", Value: "platform"}}
			}),
			argv: tstRunArgv([]string{
				"--label", "com.docker.compose.project=proj",
				"--label", "com.docker.compose.service=web",
				"--annotation", "com.example.owner=platform",
			}),
		},
		{
			name: "env",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Env = []plan.KV{{Key: "A", Value: "1"}, {Key: "B", Value: "x=y"}}
			}),
			argv: tstRunArgv([]string{"--env", "A=1", "--env", "B=x=y"}),
		},
		{
			name: "workdir and user",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.WorkingDir = "/srv"
				c.User = "101:101"
			}),
			argv: tstRunArgv([]string{"--workdir", "/srv", "--user", "101:101"}),
		},
		{
			name: "tty and stdin",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Tty = true
				c.StdinOpen = true
			}),
			argv: tstRunArgv([]string{"-t", "-i"}),
		},
		{
			name: "init nil",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Init = nil }),
			argv: tstRunArgv(nil),
		},
		{
			name: "init true",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Init = tstPtr(true) }),
			argv: tstRunArgv([]string{"--init"}),
		},
		{
			name: "init false",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Init = tstPtr(false) }),
			argv: tstRunArgv(nil),
		},
		{
			name: "stop signal and timeout rounded up",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.StopSignal = "SIGQUIT"
				c.StopTimeout = tstPtr(1500 * time.Millisecond)
			}),
			argv: tstRunArgv([]string{"--stop-signal", "SIGQUIT", "--stop-timeout", "2"}),
		},
		{
			name: "restart on-failure",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Restart = "on-failure:3" }),
			argv: tstRunArgv([]string{"--restart", "on-failure:3"}),
		},
		{
			name: "hostname domainname and dns",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Hostname = "web"
				c.DomainName = "example.test"
				c.DNS = []string{"1.1.1.1"}
				c.DNSSearch = []string{"example.test"}
				c.DNSOpts = []string{"ndots:2"}
				c.ExtraHosts = []string{"db:10.0.0.5"}
			}),
			argv: tstRunArgv([]string{
				"--hostname", "web", "--domainname", "example.test",
				"--dns", "1.1.1.1", "--dns-search", "example.test", "--dns-option", "ndots:2",
				"--add-host", "db:10.0.0.5",
			}),
		},
		{
			name: "expose and links",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Expose = []string{"8081"}
				c.Links = []string{"proj-db-1:db"}
			}),
			argv: tstRunArgv([]string{"--expose", "8081", "--link", "proj-db-1:db"}),
		},
		{
			name: "security flags",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Privileged = true
				c.CapAdd = []string{"NET_ADMIN"}
				c.CapDrop = []string{"ALL"}
				c.SecurityOpt = []string{"no-new-privileges:true"}
				c.GroupAdd = []string{"audio"}
				c.DeviceCgroupRules = []string{"c 10:229 rwm"}
				c.Devices = []plan.Device{{Source: "/dev/fuse", Target: "/dev/fuse", Permissions: "rwm"}, {Source: "/dev/null"}}
			}),
			argv: tstRunArgv([]string{
				"--privileged", "--cap-add", "NET_ADMIN", "--cap-drop", "ALL",
				"--security-opt", "no-new-privileges:true", "--group-add", "audio",
				"--device-cgroup-rule", "c 10:229 rwm",
				"--device", "/dev/fuse:/dev/fuse:rwm", "--device", "/dev/null",
			}),
		},
		{
			name: "namespaces and runtime",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Sysctls = []plan.KV{{Key: "net.core.somaxconn", Value: "1024"}}
				c.Ulimits = []plan.Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}, {Name: "nproc", Soft: 100}}
				c.Pid = "host"
				c.Ipc = "shareable"
				c.Uts = "host"
				c.Userns = "host"
				c.CgroupParent = "/compose"
				c.Cgroup = "private"
				c.Runtime = "runc"
				c.Isolation = "default"
			}),
			argv: tstRunArgv([]string{
				"--sysctl", "net.core.somaxconn=1024",
				"--ulimit", "nofile=1024:2048", "--ulimit", "nproc=100",
				"--pid", "host", "--ipc", "shareable", "--uts", "host", "--userns", "host",
				"--cgroup-parent", "/compose", "--cgroupns", "private",
				"--runtime", "runc", "--isolation", "default",
			}),
		},
		{
			name: "logging",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Logging = &plan.Logging{Driver: "json-file", Options: []plan.KV{{Key: "max-file", Value: "3"}, {Key: "max-size", Value: "10m"}}}
			}),
			argv: tstRunArgv([]string{"--log-driver", "json-file", "--log-opt", "max-file=3", "--log-opt", "max-size=10m"}),
		},
	})
}

func TestRunPorts(t *testing.T) {
	tstRender(t, []tstRenderCase{{
		name: "four port shapes and a range",
		step: tstRunStep(func(c *plan.ContainerSpec) {
			c.Ports = []plan.Port{
				{Target: 80},
				{Published: "8080", Target: 80, Protocol: "tcp"},
				{HostIP: "127.0.0.1", Published: "8080", Target: 80},
				{HostIP: "127.0.0.1", Target: 80, Protocol: "udp"},
				{Published: "8000-8010", Target: 8000},
			}
		}),
		argv: tstRunArgv([]string{
			"-p", "80",
			"-p", "8080:80/tcp",
			"-p", "127.0.0.1:8080:80",
			"-p", "127.0.0.1::80/udp",
			"-p", "8000-8010:8000",
		}),
	}})
}

func TestRunMounts(t *testing.T) {
	bindFull := func(c *plan.ContainerSpec) {
		c.Mounts = []plan.Mount{{
			Type: "bind", Source: "/host", Target: "/ctr", ReadOnly: true,
			Bind: &plan.BindOptions{Propagation: "rprivate", SELinux: "z", CreateHostPath: true, Recursive: "enabled"},
		}}
	}
	volumeFull := func(c *plan.ContainerSpec) {
		c.Mounts = []plan.Mount{{
			Type: "volume", Source: "data", Target: "/data", ReadOnly: true,
			Volume: &plan.VolumeOptions{NoCopy: true, Subpath: "html"},
		}}
	}
	imageFull := func(c *plan.ContainerSpec) {
		c.Mounts = []plan.Mount{{Type: "image", Source: "nginx:1.27", Target: "/img", Image: &plan.ImageOptions{Subpath: "usr/share"}}}
	}
	tmpfsFull := func(c *plan.ContainerSpec) {
		c.Mounts = []plan.Mount{{Type: "tmpfs", Target: "/tmp", Tmpfs: &plan.TmpfsOptions{Size: 64 << 20, Mode: 0o1777}}}
	}
	tstRender(t, []tstRenderCase{
		{
			name: "bind with ro selinux and propagation",
			step: tstRunStep(bindFull),
			argv: tstRunArgv([]string{"-v", "/host:/ctr:ro,z,rprivate"}),
		},
		{
			name: "bind plain",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/a", Target: "/b"}}
			}),
			argv: tstRunArgv([]string{"-v", "/a:/b"}),
		},
		{
			name: "bind read-only",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/a", Target: "/b", ReadOnly: true}}
			}),
			argv: tstRunArgv([]string{"-v", "/a:/b:ro"}),
		},
		{
			name: "volume mount",
			step: tstRunStep(volumeFull),
			argv: tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data,readonly,volume-nocopy,volume-subpath=html"}),
		},
		{
			name: "anonymous volume mount",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "volume", Target: "/data"}}
			}),
			argv: tstRunArgv([]string{"--mount", "type=volume,target=/data"}),
		},
		{
			name: "tmpfs mount",
			step: tstRunStep(tmpfsFull),
			argv: tstRunArgv([]string{"--mount", "type=tmpfs,target=/tmp,tmpfs-size=67108864,tmpfs-mode=1777"}),
		},
		{
			name: "image mount",
			step: tstRunStep(imageFull),
			argv: tstRunArgv([]string{"--mount", "type=image,source=nginx:1.27,target=/img,image-subpath=usr/share"}),
		},
		{
			name: "tmpfs paths",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Tmpfs = []string{"/run", "/tmp:size=1m"} }),
			argv: tstRunArgv([]string{"--tmpfs", "/run", "--tmpfs", "/tmp:size=1m"}),
		},
		{
			name: "shm size read-only volumes-from",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.ShmSize = 64 << 20
				c.VolumesFrom = []string{"proj-data-1"}
				c.VolumeDriver = "local"
				c.StorageOpt = []plan.KV{{Key: "size", Value: "10G"}}
				c.ReadOnly = true
			}),
			argv: tstRunArgv([]string{"--shm-size", "67108864", "--volumes-from", "proj-data-1", "--volume-driver", "local", "--storage-opt", "size=10G", "--read-only"}),
		},
		{
			name: "consistency and volume labels are ignored",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{
					Type: "volume", Source: "data", Target: "/data", Consistency: "cached",
					Volume: &plan.VolumeOptions{Labels: []plan.KV{{Key: "tier", Value: "web"}}},
				}}
			}),
			argv:  tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./data.consistency"), tstIgnored(tstWeb, "volumes./data.volume.labels")},
		},
		{
			name: "bind recursive disabled is ignored",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/a", Target: "/b", Bind: &plan.BindOptions{Recursive: "disabled"}}}
			}),
			argv:  tstRunArgv([]string{"-v", "/a:/b"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./b.bind.recursive")},
		},
		{
			name: "volume nocopy is ignored", v: tstMissing("run --mount volume-nocopy"),
			step:  tstRunStep(volumeFull),
			argv:  tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data,readonly,volume-subpath=html"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "volumes./data.volume.nocopy", "--mount volume-nocopy")},
		},
		{
			name: "volume subpath is ignored", v: tstMissing("run --mount volume-subpath"),
			step:  tstRunStep(volumeFull),
			argv:  tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data,readonly,volume-nocopy"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "volumes./data.volume.subpath", "--mount volume-subpath")},
		},
		{
			name: "bind keeps only ro", v: tstMissing("run --mount bind-selinux", "run --mount bind-propagation"),
			step:  tstRunStep(bindFull),
			argv:  tstRunArgv([]string{"-v", "/host:/ctr:ro"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "volumes./ctr.bind.propagation", "--mount bind-propagation"), tstNoFlag(tstWeb, "volumes./ctr.bind.selinux", "--mount bind-selinux")},
		},
		{
			name: "image subpath is ignored", v: tstMissing("run --mount image-subpath"),
			step:  tstRunStep(imageFull),
			argv:  tstRunArgv([]string{"--mount", "type=image,source=nginx:1.27,target=/img"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "volumes./img.image.subpath", "--mount image-subpath")},
		},
		{
			name: "tmpfs size and mode are ignored", v: tstMissing("run --mount tmpfs-size", "run --mount tmpfs-mode"),
			step:  tstRunStep(tmpfsFull),
			argv:  tstRunArgv([]string{"--mount", "type=tmpfs,target=/tmp"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "volumes./tmp.tmpfs.mode", "--mount tmpfs-mode"), tstNoFlag(tstWeb, "volumes./tmp.tmpfs.size", "--mount tmpfs-size")},
		},
	})
}

func TestRunHealthcheck(t *testing.T) {
	health := func(h plan.Healthcheck) func(c *plan.ContainerSpec) {
		return func(c *plan.ContainerSpec) { c.Healthcheck = &h }
	}
	full := plan.Healthcheck{
		Test:          []string{"CMD-SHELL", "curl -f http://x/ || exit 1"},
		Interval:      30 * time.Second,
		Timeout:       5 * time.Second,
		StartPeriod:   10 * time.Second,
		StartInterval: 2 * time.Second,
		Retries:       3,
	}
	fullArgv := tstRunArgv([]string{
		"--health-cmd", "curl -f http://x/ || exit 1",
		"--health-interval", "30s", "--health-timeout", "5s",
		"--health-start-period", "10s", "--health-start-interval", "2s",
		"--health-retries", "3",
	})
	tstRender(t, []tstRenderCase{
		{
			name: "cmd-shell with interval and retries",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD-SHELL", "curl -f http://x/ || exit 1"}, Interval: 30 * time.Second, Retries: 3})),
			argv: tstRunArgv([]string{"--health-cmd", "curl -f http://x/ || exit 1", "--health-interval", "30s", "--health-retries", "3"}),
		},
		{
			name: "cmd list is joined",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD", "curl", "-f", "http://x/"}})),
			argv: tstRunArgv([]string{"--health-cmd", "curl -f http://x/"}),
		},
		{
			name: "cmd list quotes whitespace",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD", "sh", "-c", "a b"}})),
			argv: tstRunArgv([]string{"--health-cmd", "sh -c 'a b'"}),
		},
		{
			name: "bare list is joined",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"curl", "-f", "http://x/"}})),
			argv: tstRunArgv([]string{"--health-cmd", "curl -f http://x/"}),
		},
		{
			name: "none disables",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"NONE"}, Interval: 30 * time.Second})),
			argv: tstRunArgv([]string{"--no-healthcheck"}),
		},
		{
			name: "disable wins over test",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD", "true"}, Retries: 3, Disable: true})),
			argv: tstRunArgv([]string{"--no-healthcheck"}),
		},
		{name: "all durations", step: tstRunStep(health(full)), argv: fullArgv},
		{name: "renders without healthcheck capability", v: tstNoHealth(), step: tstRunStep(health(full)), argv: fullArgv},
		{
			name: "start interval is ignored", v: tstMissing("run --health-start-interval"),
			step: tstRunStep(health(full)),
			argv: tstRunArgv([]string{
				"--health-cmd", "curl -f http://x/ || exit 1",
				"--health-interval", "30s", "--health-timeout", "5s",
				"--health-start-period", "10s",
				"--health-retries", "3",
			}),
			warns: []tstWarn{tstNoFlag(tstWeb, "healthcheck.start_interval", "--health-start-interval")},
		},
	})
}

// tstTwoNetworks attaches front (aliases web and a) and back (alias web, an ipv4 address).
func tstTwoNetworks(c *plan.ContainerSpec) {
	c.Networks = []plan.NetworkAttachment{
		{Key: "front", Name: "proj_front", Aliases: []string{"web", "a"}},
		{Key: "back", Name: "proj_back", Aliases: []string{"web"}, IPv4: "10.0.1.5"},
	}
}

func TestRunNetworks(t *testing.T) {
	aliasFree := tstAliasHostname()
	aliasFree.missing = nil
	tstRender(t, []tstRenderCase{
		{
			name: "first network flags and second network keys",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{
					{Key: "front", Name: "proj_front", Aliases: []string{"web", "a"}, IPv4: "10.0.0.5"},
					{Key: "back", Name: "proj_back", Aliases: []string{"web"}, IPv4: "10.0.1.5"},
				}
			}),
			argv: tstRunArgv([]string{
				"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--ip", "10.0.0.5",
				"--network", "name=proj_back,alias=web,ip=10.0.1.5",
			}),
		},
		{
			name: "every address field",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{
					{Key: "front", Name: "proj_front", Aliases: []string{"web"}, IPv4: "10.0.0.5", IPv6: "fd00::5", MacAddress: "02:42:ac:1c:00:0a", LinkLocalIPs: []string{"169.254.0.5", "169.254.0.6"}},
					{Key: "back", Name: "proj_back", Aliases: []string{"x", "y"}, IPv4: "10.0.1.5", IPv6: "fd00:1::5", MacAddress: "02:42:ac:1d:00:0a", LinkLocalIPs: []string{"169.254.1.5"}},
				}
			}),
			argv: tstRunArgv([]string{
				"--network", "proj_front", "--network-alias", "web",
				"--ip", "10.0.0.5", "--ip6", "fd00::5", "--mac-address", "02:42:ac:1c:00:0a",
				"--link-local-ip", "169.254.0.5", "--link-local-ip", "169.254.0.6",
				"--network", "name=proj_back,alias=x,alias=y,ip=10.0.1.5,ip6=fd00:1::5,mac-address=02:42:ac:1d:00:0a,link-local-ip=169.254.1.5",
			}),
		},
		{
			name: "driver opts gw priority and interface name are ignored",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{
					Key: "front", Name: "proj_front", Priority: 100, GwPriority: 1, InterfaceName: "eth0",
					DriverOpts: []plan.KV{{Key: "com.docker.network.endpoint.ifname", Value: "eth0"}},
				}}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front"}),
			warns: []tstWarn{
				tstIgnored(tstWeb, "networks.front.driver_opts"),
				tstIgnored(tstWeb, "networks.front.gw_priority"),
				tstIgnored(tstWeb, "networks.front.interface_name"),
			},
		},
		{
			name: "network precedes hostname",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Hostname = "custom"
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web"}}}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--hostname", "custom"}),
		},
		{
			name: "alias all unions aliases on the first network and ignores second network ip", v: tstAliasAll(),
			step:  tstRunStep(tstTwoNetworks),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "proj_back"}),
			warns: []tstWarn{tstIgnoredFor(tstWeb, "networks.back.ipv4_address", "stub takes --ip for the first network only")},
		},
		{
			name: "alias all moves later aliases to the first network silently", v: tstAliasAll(),
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{
					{Key: "front", Name: "proj_front", Aliases: []string{"web"}},
					{Key: "back", Name: "proj_back", Aliases: []string{"web", "api"}},
				}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "api", "--network", "proj_back"}),
		},
		{
			name: "alias hostname turns the service alias into hostname and ignores the rest", v: tstAliasHostname(),
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web", "a"}}}
			}),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--hostname", "web"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "networks.front.aliases", "--network-alias")},
		},
		{
			name: "alias hostname with the service alias alone is silent", v: tstAliasHostname(),
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web"}}}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front", "--hostname", "web"}),
		},
		{
			name: "alias hostname keeps an explicit hostname and ignores aliases", v: tstAliasHostname(),
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web"}}}
				c.Hostname = "custom"
			}),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--hostname", "custom"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "networks.front.aliases", "--network-alias")},
		},
		{
			name: "alias hostname without aliases sets no hostname", v: tstAliasHostname(),
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front"}}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front"}),
		},
		{
			name: "alias hostname renders the remaining aliases when --network-alias exists", v: aliasFree,
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web", "a"}}}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front", "--network-alias", "a", "--hostname", "web"}),
		},
		{
			name: "alias hostname removes the service alias on every network", v: aliasFree,
			step: tstRunStep(tstTwoNetworks),
			argv: tstRunArgv([]string{"--network", "proj_front", "--network-alias", "a", "--network", "name=proj_back,ip=10.0.1.5", "--hostname", "web"}),
		},
		{
			name: "network mode wins over networks",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.NetworkMode = "host"
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front"}}
			}),
			argv:  tstRunArgv([]string{"--network", "host"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks")},
		},
		{
			name: "network mode alone",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.NetworkMode = "bridge" }),
			argv: tstRunArgv([]string{"--network", "bridge"}),
		},
	})
}

func TestRunNetworkKeys(t *testing.T) {
	noKeys := tstFull()
	noKeys.networkKeys = nil
	aliasKey := tstFull()
	aliasKey.networkKeys = []string{"alias"}
	aliasKeyNoIP := tstMissing("run --ip")
	aliasKeyNoIP.networkKeys = []string{"alias"}
	reversed := tstFull()
	reversed.networkKeys = []string{"link-local-ip", "mac-address", "ip6", "ip", "alias"}
	everyField := func(c *plan.ContainerSpec) {
		c.Networks = []plan.NetworkAttachment{
			{Key: "front", Name: "proj_front", Aliases: []string{"web"}},
			{Key: "back", Name: "proj_back", Aliases: []string{"x"}, IPv4: "10.0.1.5", IPv6: "fd00:1::5", MacAddress: "02:42:ac:1d:00:0a", LinkLocalIPs: []string{"169.254.1.5"}},
		}
	}
	tstRender(t, []tstRenderCase{
		{
			name: "no keys warn for every second network attribute", v: noKeys,
			step: tstRunStep(tstTwoNetworks),
			argv: tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "proj_back"}),
			warns: []tstWarn{
				tstIgnoredFor(tstWeb, "networks.back.aliases", "stub takes --network-alias for the first network only"),
				tstIgnoredFor(tstWeb, "networks.back.ipv4_address", "stub takes --ip for the first network only"),
			},
		},
		{
			name: "alias key only keeps alias", v: aliasKey,
			step:  tstRunStep(tstTwoNetworks),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "name=proj_back,alias=web"}),
			warns: []tstWarn{tstIgnoredFor(tstWeb, "networks.back.ipv4_address", "stub takes --ip for the first network only")},
		},
		{
			name: "missing flag warning wins over the key list", v: aliasKeyNoIP,
			step:  tstRunStep(tstTwoNetworks),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "name=proj_back,alias=web"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "networks.back.ipv4_address", "--ip")},
		},
		{
			name: "missing flag with every key", v: tstMissing("run --ip"),
			step:  tstRunStep(tstTwoNetworks),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "name=proj_back,alias=web"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "networks.back.ipv4_address", "--ip")},
		},
		{
			name: "keys render in a fixed order whatever the list order", v: reversed,
			step: tstRunStep(everyField),
			argv: tstRunArgv([]string{
				"--network", "proj_front", "--network-alias", "web",
				"--network", "name=proj_back,alias=x,ip=10.0.1.5,ip6=fd00:1::5,mac-address=02:42:ac:1d:00:0a,link-local-ip=169.254.1.5",
			}),
		},
	})
}

func TestRunResources(t *testing.T) {
	cpus := func(v float64) func(c *plan.ContainerSpec) {
		return func(c *plan.ContainerSpec) { c.CPUs = v }
	}
	tstRender(t, []tstRenderCase{
		{
			name: "integer cpus round a fraction up", v: tstIntCPUs(),
			step:  tstRunStep(cpus(1.5)),
			argv:  tstRunArgv([]string{"--cpus", "2"}),
			warns: []tstWarn{tstDegradedFor(tstWeb, "cpus", "2", "stub takes whole cpus")},
		},
		{
			name: "integer cpus round a half up", v: tstIntCPUs(),
			step:  tstRunStep(cpus(0.5)),
			argv:  tstRunArgv([]string{"--cpus", "1"}),
			warns: []tstWarn{tstDegradedFor(tstWeb, "cpus", "1", "stub takes whole cpus")},
		},
		{name: "integer cpus keep a whole value silently", v: tstIntCPUs(), step: tstRunStep(cpus(2)), argv: tstRunArgv([]string{"--cpus", "2"})},
		{name: "fractional cpus", step: tstRunStep(cpus(1.5)), argv: tstRunArgv([]string{"--cpus", "1.5"})},
		{name: "whole cpus", step: tstRunStep(cpus(2)), argv: tstRunArgv([]string{"--cpus", "2"})},
		{
			name: "cpu flags",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.CPUShares = 512
				c.CPUSet = "0-1"
				c.CPUPeriod = 100000
				c.CPUQuota = 150000
				c.CPURTPeriod = 1000000
				c.CPURTRuntime = 950000
				c.CPUCount = 2
				c.CPUPercent = 50
			}),
			argv: tstRunArgv([]string{
				"--cpu-shares", "512", "--cpuset-cpus", "0-1",
				"--cpu-period", "100000", "--cpu-quota", "150000",
				"--cpu-rt-period", "1000000", "--cpu-rt-runtime", "950000",
				"--cpu-count", "2", "--cpu-percent", "50",
			}),
		},
		{
			name: "memory and pids",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Memory = 512 << 20
				c.MemoryReservation = 256 << 20
				c.MemorySwap = 1 << 30
				c.MemorySwappiness = 60
				c.OomKillDisable = true
				c.OomScoreAdj = 100
				c.PidsLimit = 100
			}),
			argv: tstRunArgv([]string{
				"--memory", "536870912", "--memory-reservation", "268435456", "--memory-swap", "1073741824",
				"--memory-swappiness", "60", "--oom-kill-disable", "--oom-score-adj", "100", "--pids-limit", "100",
			}),
		},
		{
			name: "blkio",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Blkio = tstBlkio() }),
			argv: tstRunArgv([]string{
				"--blkio-weight", "500", "--blkio-weight-device", "/dev/sda:400",
				"--device-read-bps", "/dev/sda:1048576", "--device-write-bps", "/dev/sdb:2097152",
				"--device-read-iops", "/dev/sda:1000", "--device-write-iops", "/dev/sdb:2000",
			}),
		},
	})
}

func tstBlkio() *plan.Blkio {
	return &plan.Blkio{
		Weight:          500,
		WeightDevice:    []plan.WeightDevice{{Path: "/dev/sda", Weight: 400}},
		DeviceReadBps:   []plan.ThrottleDevice{{Path: "/dev/sda", Rate: 1 << 20}},
		DeviceWriteBps:  []plan.ThrottleDevice{{Path: "/dev/sdb", Rate: 2 << 20}},
		DeviceReadIOps:  []plan.ThrottleDevice{{Path: "/dev/sda", Rate: 1000}},
		DeviceWriteIOps: []plan.ThrottleDevice{{Path: "/dev/sdb", Rate: 2000}},
	}
}

func TestRunGpus(t *testing.T) {
	step := tstRunStep(func(c *plan.ContainerSpec) {
		c.Gpus = []plan.DeviceRequest{{Driver: "nvidia", Count: 1, IDs: []string{"0"}, Capabilities: []string{"gpu"}}}
	})
	argv, warnings, err := dockercli.New(tstFull()).Render(step)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	i := slices.Index(argv, "--gpus")
	if i < 0 || i+1 >= len(argv) || argv[i+1] == "" || strings.HasPrefix(argv[i+1], "-") {
		t.Errorf("argv = %q, want --gpus followed by a value", argv)
	}
	tstCheckWarnings(t, warnings, nil)
}

// TestGaps checks that a flag Has denies drops its tokens and warns exactly once with the variant's name.
func TestGaps(t *testing.T) {
	bare := tstBare()
	gap := func(label, attribute, flag string, f func(c *plan.ContainerSpec)) tstRenderCase {
		return tstRenderCase{name: label + "/" + attribute + " is ignored", v: bare, step: tstRunStep(f), argv: tstRunArgv(nil), warns: []tstWarn{tstNoFlag(tstWeb, attribute, flag)}}
	}
	other := tstMissing("run --restart")
	other.name = "other"
	tstRender(t, []tstRenderCase{
		{
			name: "missing/restart is ignored", v: tstMissing("run --restart"),
			step:  tstRunStep(func(c *plan.ContainerSpec) { c.Restart = "always" }),
			argv:  tstRunArgv(nil),
			warns: []tstWarn{tstNoFlag(tstWeb, "restart", "--restart")},
		},
		{
			name: "missing/warning names the variant", v: other,
			step:  tstRunStep(func(c *plan.ContainerSpec) { c.Restart = "always" }),
			argv:  tstRunArgv(nil),
			warns: []tstWarn{tstIgnoredFor(tstWeb, "restart", "other has no --restart")},
		},
		{
			name: "missing/present flags still render", v: tstMissing("run --restart"),
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Restart = "always"
				c.Privileged = true
			}),
			argv:  tstRunArgv([]string{"--privileged"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "restart", "--restart")},
		},
		gap("bare", "restart", "--restart", func(c *plan.ContainerSpec) { c.Restart = "always" }),
		gap("bare", "privileged", "--privileged", func(c *plan.ContainerSpec) { c.Privileged = true }),
		gap("bare", "cap_add", "--cap-add", func(c *plan.ContainerSpec) { c.CapAdd = []string{"NET_ADMIN"} }),
		gap("bare", "logging", "--log-driver", func(c *plan.ContainerSpec) {
			c.Logging = &plan.Logging{Driver: "json-file", Options: []plan.KV{{Key: "max-size", Value: "10m"}}}
		}),
		gap("bare", "annotations", "--annotation", func(c *plan.ContainerSpec) {
			c.Annotations = []plan.KV{{Key: "com.example.owner", Value: "platform"}}
		}),
		gap("bare", "init", "--init", func(c *plan.ContainerSpec) { c.Init = tstPtr(true) }),
		gap("bare", "platform", "--platform", func(c *plan.ContainerSpec) { c.Platform = "linux/amd64" }),
		gap("bare", "mac_address", "--mac-address", func(c *plan.ContainerSpec) { c.MacAddress = "02:42:ac:1c:00:0a" }),
		gap("bare", "links", "--link", func(c *plan.ContainerSpec) { c.Links = []string{"proj-db-1:db"} }),
		gap("bare", "expose", "--expose", func(c *plan.ContainerSpec) { c.Expose = []string{"8081"} }),
		gap("bare", "isolation", "--isolation", func(c *plan.ContainerSpec) { c.Isolation = "default" }),
		gap("bare", "cpu_count", "--cpu-count", func(c *plan.ContainerSpec) { c.CPUCount = 2 }),
		gap("bare", "blkio_config", "--blkio-weight", func(c *plan.ContainerSpec) { c.Blkio = tstBlkio() }),
		gap("bare", "healthcheck.start_interval", "--health-start-interval", func(c *plan.ContainerSpec) {
			c.Healthcheck = &plan.Healthcheck{StartInterval: 2 * time.Second}
		}),
		{
			name: "bare/build privileged is ignored", v: bare,
			step:  plan.BuildImage{Build: plan.BuildSpec{Service: "web", Tag: "proj-web", Context: ".", Privileged: true}},
			argv:  []string{"build", "-t", "proj-web", "."},
			warns: []tstWarn{tstNoFlag(tstWeb, "build.privileged", "--allow")},
		},
		{
			name: "bare/network enable_ipv4 false is ignored", v: bare,
			step:  plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_default", EnableIPv4: tstPtr(false)}},
			argv:  []string{"network", "create", "proj_default"},
			warns: []tstWarn{tstNoFlag("networks.proj_default", "enable_ipv4", "--ipv4")},
		},
		{
			name: "bare/volume driver is ignored", v: bare,
			step:  plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data", Driver: "local"}},
			argv:  []string{"volume", "create", "proj_data"},
			warns: []tstWarn{tstNoFlag("volumes.proj_data", "driver", "--driver")},
		},
	})
}

func tstNetworkStep() plan.CreateNetwork {
	return plan.CreateNetwork{Network: plan.NetworkSpec{
		Name:       "proj_front",
		Driver:     "bridge",
		DriverOpts: []plan.KV{{Key: "com.docker.network.bridge.name", Value: "br-front"}},
		IPAMDriver: "default",
		IPAM: []plan.IPAMPool{{
			Subnet: "172.28.0.0/16", Gateway: "172.28.0.1", IPRange: "172.28.5.0/24",
			AuxAddresses: []plan.KV{{Key: "router", Value: "172.28.1.5"}},
		}},
		IPAMOptions: []plan.KV{{Key: "com.example.pool", Value: "front"}},
		Internal:    true,
		Attachable:  true,
		EnableIPv4:  tstPtr(false),
		EnableIPv6:  tstPtr(true),
		Labels:      []plan.KV{{Key: plan.LabelNetwork, Value: "front"}, {Key: plan.LabelProject, Value: "proj"}},
	}}
}

func tstVolumeStep() plan.CreateVolume {
	return plan.CreateVolume{Volume: plan.VolumeSpec{
		Name:       "proj_data",
		Driver:     "local",
		DriverOpts: []plan.KV{{Key: "device", Value: "tmpfs"}, {Key: "o", Value: "size=100m"}, {Key: "type", Value: "tmpfs"}},
		Labels:     []plan.KV{{Key: plan.LabelProject, Value: "proj"}, {Key: plan.LabelVolume, Value: "data"}},
	}}
}

func tstBuildStep() plan.BuildImage {
	return plan.BuildImage{Build: plan.BuildSpec{
		Service:            "web",
		Tag:                "proj-web",
		Context:            "/srv/proj/web",
		Dockerfile:         "/srv/proj/web/Dockerfile",
		Args:               []plan.KV{{Key: "NGINX_VERSION", Value: "1.27"}},
		Target:             "runtime",
		Labels:             []plan.KV{{Key: "org.opencontainers.image.title", Value: "web"}},
		NoCache:            true,
		Pull:               true,
		Platforms:          []string{"linux/amd64", "linux/arm64"},
		CacheFrom:          []string{"type=registry,ref=registry.example.test/proj-web:cache"},
		CacheTo:            []string{"type=inline"},
		ExtraHosts:         []string{"db:10.0.0.5"},
		Network:            "host",
		ShmSize:            64 << 20,
		SSH:                []string{"default"},
		Secrets:            []plan.BuildSecret{{ID: "npmrc", Source: "/srv/proj/.npmrc"}, {ID: "token"}},
		AdditionalContexts: []plan.KV{{Key: "assets", Value: "/srv/proj/assets"}},
		Tags:               []string{"registry.example.test/proj-web:1.0"},
		Ulimits:            []plan.Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}},
		Isolation:          "default",
		Privileged:         true,
	}}
}

func TestOtherSteps(t *testing.T) {
	buildCommon := []string{
		"build", "-t", "proj-web", "-t", "registry.example.test/proj-web:1.0", "-f", "/srv/proj/web/Dockerfile",
		"--build-arg", "NGINX_VERSION=1.27", "--target", "runtime",
		"--label", "org.opencontainers.image.title=web", "--no-cache", "--pull",
		"--platform", "linux/amd64,linux/arm64",
		"--cache-from", "type=registry,ref=registry.example.test/proj-web:cache", "--cache-to", "type=inline",
		"--ssh", "default", "--secret", "id=npmrc,src=/srv/proj/.npmrc", "--secret", "id=token",
		"--network", "host", "--shm-size", "67108864", "--add-host", "db:10.0.0.5",
		"--ulimit", "nofile=1024:2048", "--isolation", "default",
		"--build-context", "assets=/srv/proj/assets",
	}
	networkCommon := []string{
		"network", "create", "--driver", "bridge", "--opt", "com.docker.network.bridge.name=br-front",
		"--ipam-driver", "default", "--subnet", "172.28.0.0/16", "--gateway", "172.28.0.1", "--ip-range", "172.28.5.0/24",
		"--aux-address", "router=172.28.1.5", "--ipam-opt", "com.example.pool=front", "--internal",
	}
	networkLabels := []string{"--label", "com.docker.compose.network=front", "--label", "com.docker.compose.project=proj", "proj_front"}
	jsonList := tstFull()
	jsonList.psNoTrunc, jsonList.listFormat = false, "json"

	tstRender(t, []tstRenderCase{
		{
			name: "create network all fields", step: tstNetworkStep(),
			argv: slices.Concat(networkCommon, []string{"--attachable", "--ipv6", "--ipv4=false"}, networkLabels),
		},
		{
			name: "create network without attachable and ipv4", v: tstMissing("network create --attachable", "network create --ipv4"), step: tstNetworkStep(),
			argv: slices.Concat(networkCommon, []string{"--ipv6"}, networkLabels),
			warns: []tstWarn{
				tstNoFlag("networks.proj_front", "attachable", "--attachable"),
				tstNoFlag("networks.proj_front", "enable_ipv4", "--ipv4"),
			},
		},
		{
			name: "create network minimal",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_default", EnableIPv4: tstPtr(true), EnableIPv6: tstPtr(false)}},
			argv: []string{"network", "create", "proj_default"},
		},
		{
			name: "create volume all fields", step: tstVolumeStep(),
			argv: []string{
				"volume", "create", "--driver", "local",
				"--opt", "device=tmpfs", "--opt", "o=size=100m", "--opt", "type=tmpfs",
				"--label", "com.docker.compose.project=proj", "--label", "com.docker.compose.volume=data", "proj_data",
			},
		},
		{
			name: "create volume without driver and opts", v: tstMissing("volume create --driver", "volume create --opt"), step: tstVolumeStep(),
			argv: []string{"volume", "create", "--label", "com.docker.compose.project=proj", "--label", "com.docker.compose.volume=data", "proj_data"},
			warns: []tstWarn{
				tstNoFlag("volumes.proj_data", "driver", "--driver"),
				tstNoFlag("volumes.proj_data", "driver_opts", "--opt"),
			},
		},
		{
			name: "create volume minimal",
			step: plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data"}},
			argv: []string{"volume", "create", "proj_data"},
		},
		{
			name: "build all fields", step: tstBuildStep(),
			argv: slices.Concat(buildCommon, []string{"--allow", "security.insecure", "/srv/proj/web"}),
		},
		{
			name: "build without allow", v: tstMissing("build --allow"), step: tstBuildStep(),
			argv:  slices.Concat(buildCommon, []string{"/srv/proj/web"}),
			warns: []tstWarn{tstNoFlag(tstWeb, "build.privileged", "--allow")},
		},
		{
			name: "build minimal",
			step: plan.BuildImage{Build: plan.BuildSpec{Service: "web", Tag: "proj-web", Context: "."}},
			argv: []string{"build", "-t", "proj-web", "."},
		},
		{name: "start many", step: plan.StartContainers{Names: []string{"a", "b"}}, argv: []string{"start", "a", "b"}},
		{name: "stop without timeout", step: plan.StopContainers{Names: []string{"a"}}, argv: []string{"stop", "a"}},
		{
			name: "stop timeout rounds up",
			step: plan.StopContainers{Names: []string{"a", "b"}, Timeout: tstPtr(1500 * time.Millisecond)},
			argv: []string{"stop", "--time", "2", "a", "b"},
		},
		{
			name: "stop whole timeout",
			step: plan.StopContainers{Names: []string{"a"}, Timeout: tstPtr(10 * time.Second)},
			argv: []string{"stop", "--time", "10", "a"},
		},
		{name: "remove containers", step: plan.RemoveContainers{Names: []string{"a", "b"}}, argv: []string{"rm", "-f", "a", "b"}},
		{name: "remove containers with volumes", step: plan.RemoveContainers{Names: []string{"a"}, Volumes: true}, argv: []string{"rm", "-f", "-v", "a"}},
		{name: "remove network", step: plan.RemoveNetwork{Name: "proj_front"}, argv: []string{"network", "rm", "proj_front"}},
		{name: "remove volume", step: plan.RemoveVolume{Name: "proj_data"}, argv: []string{"volume", "rm", "proj_data"}},
		{name: "list containers without project", step: plan.ListContainers{}, argv: []string{"ps", "-a", "--no-trunc", "--format", "{{json .}}"}},
		{
			name: "list containers with project", step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--no-trunc", "--filter", "label=com.docker.compose.project=proj", "--format", "{{json .}}"},
		},
		{name: "json/list containers without project", v: jsonList, step: plan.ListContainers{}, argv: []string{"ps", "-a", "--format", "json"}},
		{
			name: "json/list containers with project", v: jsonList, step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--filter", "label=com.docker.compose.project=proj", "--format", "json"},
		},
		{name: "list networks", step: plan.ListNetworks{}, argv: []string{"network", "ls", "--format", "{{json .}}"}},
		{name: "json/list networks", v: jsonList, step: plan.ListNetworks{}, argv: []string{"network", "ls", "--format", "json"}},
		{name: "list volumes", step: plan.ListVolumes{}, argv: []string{"volume", "ls", "--format", "{{json .}}"}},
		{name: "json/list volumes", v: jsonList, step: plan.ListVolumes{}, argv: []string{"volume", "ls", "--format", "json"}},
		{name: "list images", step: plan.ListImages{}, argv: []string{"image", "ls", "--format", "{{json .}}"}},
		{name: "json/list images", v: jsonList, step: plan.ListImages{}, argv: []string{"image", "ls", "--format", "json"}},
		{name: "inspect", step: plan.InspectContainer{Name: "proj-web-1"}, argv: []string{"inspect", "proj-web-1"}},
		{name: "logs plain", step: plan.Logs{Name: "proj-web-1"}, argv: []string{"logs", "proj-web-1"}},
		{name: "logs tail all is omitted", step: plan.Logs{Name: "proj-web-1", Tail: "all"}, argv: []string{"logs", "proj-web-1"}},
		{name: "logs follow and tail", step: plan.Logs{Name: "proj-web-1", Follow: true, Tail: "10"}, argv: []string{"logs", "-f", "--tail", "10", "proj-web-1"}},
	})
}

// tstFullRunStep sets every RunContainer attribute a Has gate guards, with two networks and one mount of each type.
func tstFullRunStep() plan.RunContainer {
	return tstRunStep(func(c *plan.ContainerSpec) {
		c.Platform = "linux/amd64"
		c.PullPolicy = "always"
		c.Labels = []plan.KV{{Key: plan.LabelProject, Value: "proj"}}
		c.Annotations = []plan.KV{{Key: "com.example.owner", Value: "platform"}}
		c.Entrypoint = []string{"/entry.sh"}
		c.Command = []string{"serve"}
		c.WorkingDir = "/srv"
		c.User = "101:101"
		c.Tty, c.StdinOpen = true, true
		c.Init = tstPtr(true)
		c.StopSignal = "SIGQUIT"
		c.StopTimeout = tstPtr(10 * time.Second)
		c.Env = []plan.KV{{Key: "A", Value: "1"}}
		c.Restart = "always"
		c.Networks = []plan.NetworkAttachment{
			{Key: "front", Name: "proj_front", Aliases: []string{"web"}, IPv4: "10.0.0.5", IPv6: "fd00::5", MacAddress: "02:42:ac:1c:00:0a", LinkLocalIPs: []string{"169.254.0.5"}},
			{Key: "back", Name: "proj_back", Aliases: []string{"web"}, IPv4: "10.0.1.5", IPv6: "fd00:1::5", MacAddress: "02:42:ac:1d:00:0a", LinkLocalIPs: []string{"169.254.1.5"}},
		}
		c.Hostname = "web"
		c.DomainName = "example.test"
		c.MacAddress = "02:42:ac:1c:00:0a"
		c.DNS = []string{"1.1.1.1"}
		c.DNSSearch = []string{"example.test"}
		c.DNSOpts = []string{"ndots:2"}
		c.ExtraHosts = []string{"db:10.0.0.5"}
		c.Ports = []plan.Port{{Target: 80}}
		c.Expose = []string{"8081"}
		c.Links = []string{"proj-db-1:db"}
		c.Mounts = []plan.Mount{
			{Type: "bind", Source: "/host", Target: "/ctr", Bind: &plan.BindOptions{Propagation: "rprivate", SELinux: "z"}},
			{Type: "volume", Source: "data", Target: "/data", Volume: &plan.VolumeOptions{NoCopy: true, Subpath: "html"}},
			{Type: "image", Source: "nginx:1.27", Target: "/img", Image: &plan.ImageOptions{Subpath: "usr/share"}},
			{Type: "tmpfs", Target: "/tmp", Tmpfs: &plan.TmpfsOptions{Size: 64 << 20, Mode: 0o1777}},
		}
		c.Tmpfs = []string{"/run"}
		c.ShmSize = 64 << 20
		c.VolumesFrom = []string{"proj-data-1"}
		c.VolumeDriver = "local"
		c.StorageOpt = []plan.KV{{Key: "size", Value: "10G"}}
		c.ReadOnly = true
		c.Privileged = true
		c.CapAdd = []string{"NET_ADMIN"}
		c.CapDrop = []string{"ALL"}
		c.SecurityOpt = []string{"no-new-privileges:true"}
		c.GroupAdd = []string{"audio"}
		c.DeviceCgroupRules = []string{"c 10:229 rwm"}
		c.Devices = []plan.Device{{Source: "/dev/fuse"}}
		c.Gpus = []plan.DeviceRequest{{Count: 1}}
		c.Sysctls = []plan.KV{{Key: "net.core.somaxconn", Value: "1024"}}
		c.Ulimits = []plan.Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}}
		c.Pid, c.Ipc, c.Uts, c.Userns = "host", "shareable", "host", "host"
		c.CgroupParent = "/compose"
		c.Cgroup = "private"
		c.Runtime = "runc"
		c.Isolation = "default"
		c.CPUs = 1.5
		c.CPUShares = 512
		c.CPUSet = "0-1"
		c.CPUPeriod, c.CPUQuota = 100000, 150000
		c.CPURTPeriod, c.CPURTRuntime = 1000000, 950000
		c.CPUCount = 2
		c.CPUPercent = 50
		c.Memory, c.MemoryReservation, c.MemorySwap, c.MemorySwappiness = 512<<20, 256<<20, 1<<30, 60
		c.OomKillDisable = true
		c.OomScoreAdj = 100
		c.PidsLimit = 100
		c.Blkio = tstBlkio()
		c.Healthcheck = &plan.Healthcheck{
			Test: []string{"CMD", "true"}, Interval: 30 * time.Second, Timeout: 5 * time.Second,
			StartPeriod: 10 * time.Second, StartInterval: 2 * time.Second, Retries: 3,
		}
		c.Logging = &plan.Logging{Driver: "json-file", Options: []plan.KV{{Key: "max-size", Value: "10m"}}}
	})
}

// TestHasAsked pins the (command, flag) keys Has is asked for fully populated steps: the four commands and the gated flags, mount sub-keys and gate-only keys.
func TestHasAsked(t *testing.T) {
	r := tstNewRecorder()
	e := dockercli.New(r)
	for _, step := range []plan.Step{tstFullRunStep(), tstBuildStep(), tstNetworkStep(), tstVolumeStep()} {
		if _, _, err := e.Render(step); err != nil {
			t.Fatalf("Render(%T) error: %v", step, err)
		}
	}
	if got, want := r.commands(), []string{"build", "network create", "run", "volume create"}; !reflect.DeepEqual(got, want) {
		t.Errorf("commands asked = %q, want %q", got, want)
	}
	for _, command := range r.commands() {
		for _, flag := range r.flags(command) {
			if !strings.HasPrefix(flag, "-") || strings.Contains(flag, "=") {
				t.Errorf("Has(%q, %q): flag must be a bare flag or a --mount sub-key", command, flag)
			}
		}
	}
	want := map[string][]string{
		"run": {
			"--add-host", "--annotation", "--blkio-weight", "--cap-add", "--cap-drop", "--cgroup-parent", "--cgroupns",
			"--cpu-count", "--cpu-percent", "--cpu-period", "--cpu-quota", "--cpu-rt-period", "--cpu-rt-runtime",
			"--cpu-shares", "--cpuset-cpus", "--device", "--device-cgroup-rule", "--expose", "--group-add",
			"--health-start-interval", "--init", "--ip", "--ip6", "--ipc", "--isolation", "--link", "--link-local-ip",
			"--log-driver", "--mac-address", "--memory-reservation", "--memory-swap", "--memory-swappiness",
			"--mount bind-propagation", "--mount bind-selinux", "--mount image-subpath", "--mount tmpfs-mode",
			"--mount tmpfs-size", "--mount volume-nocopy", "--mount volume-subpath", "--network-alias",
			"--oom-kill-disable", "--oom-score-adj", "--pid", "--pids-limit", "--platform", "--privileged",
			"--read-only", "--restart", "--runtime", "--security-opt", "--storage-opt", "--sysctl", "--userns",
			"--uts", "--volume-driver", "--volumes-from",
		},
		"build": {
			"--add-host", "--allow", "--build-context", "--cache-from", "--cache-to", "--isolation", "--network",
			"--platform", "--secret", "--shm-size", "--ssh", "--ulimit",
		},
		"network create": {"--attachable", "--aux-address", "--ipam-driver", "--ipam-opt", "--ipv4", "--ipv6"},
		"volume create":  {"--driver", "--opt"},
	}
	for command, flags := range want {
		got := r.flags(command)
		for _, flag := range flags {
			if !slices.Contains(got, flag) {
				t.Errorf("Has(%q, %q) was never asked; asked %q", command, flag, got)
			}
		}
	}
}

// TestHasNotAskedForUnset checks that Has is never asked about attributes that are unset or zero.
func TestHasNotAskedForUnset(t *testing.T) {
	r := tstNewRecorder()
	e := dockercli.New(r)
	steps := []plan.Step{
		tstRunStep(nil),
		tstRunStep(func(c *plan.ContainerSpec) {
			c.PullPolicy = "missing"
			c.Init = tstPtr(false)
			c.Entrypoint = []string{}
			c.Command = []string{}
			c.Restart = ""
		}),
		plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_default", EnableIPv4: tstPtr(true), EnableIPv6: tstPtr(false)}},
		plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data"}},
		plan.BuildImage{Build: plan.BuildSpec{Service: "web", Tag: "proj-web", Context: "."}},
	}
	for _, step := range steps {
		argv, _, err := e.Render(step)
		if err != nil || len(argv) == 0 {
			t.Fatalf("Render(%T) = %q, %v; want a command and nil error", step, argv, err)
		}
		if len(r.asked) > 0 {
			t.Errorf("Render(%T) asked Has about %v, want nothing", step, r.asked)
		}
	}
}

func TestRenderErrors(t *testing.T) {
	cases := []struct {
		name string
		step plan.Step
	}{
		{"nil step", nil},
		{"unknown step", struct{ plan.Step }{}},
		{"run without name", plan.RunContainer{Container: plan.ContainerSpec{Service: "web", Image: "img"}}},
		{"run without image", plan.RunContainer{Container: plan.ContainerSpec{Service: "web", Name: "n"}}},
		{"start without names", plan.StartContainers{}},
		{"stop without names", plan.StopContainers{}},
		{"remove containers without names", plan.RemoveContainers{}},
		{"remove network without name", plan.RemoveNetwork{}},
		{"remove volume without name", plan.RemoveVolume{}},
		{"inspect without name", plan.InspectContainer{}},
		{"logs without name", plan.Logs{}},
		{"create network without name", plan.CreateNetwork{}},
		{"create volume without name", plan.CreateVolume{}},
		{"build without context", plan.BuildImage{Build: plan.BuildSpec{Service: "web", Tag: "proj-web"}}},
		{"build without tag", plan.BuildImage{Build: plan.BuildSpec{Service: "web", Context: "."}}},
	}
	for _, cfg := range []tstConfig{{"full", tstFull()}, {"bare", tstBare()}} {
		e := dockercli.New(cfg.v)
		for _, tt := range cases {
			t.Run(cfg.label+"/"+tt.name, func(t *testing.T) {
				argv, warnings, err := e.Render(tt.step)
				if err == nil {
					t.Errorf("Render(%T) = %q, nil error; want an error", tt.step, argv)
				}
				if argv != nil || warnings != nil {
					t.Errorf("Render(%T) = %q, %q; want nil argv and nil warnings", tt.step, argv, warnings)
				}
			})
		}
	}
}

func TestDeterministic(t *testing.T) {
	for _, cfg := range tstConfigs() {
		t.Run(cfg.label, func(t *testing.T) {
			e := dockercli.New(cfg.v)
			for _, step := range enginetest.Steps() {
				argv, warnings, err := e.Render(step)
				if _, wait := step.(plan.WaitContainer); err != nil || (!wait && len(argv) == 0) {
					t.Fatalf("Render(%T) = %q, %v; want a command and nil error", step, argv, err)
				}
				for i := range 100 {
					argv2, warnings2, err2 := e.Render(step)
					if err2 != nil || !reflect.DeepEqual(argv, argv2) || !reflect.DeepEqual(warnings, warnings2) {
						t.Fatalf("Render(%T) call %d = %q, %q, %v; first call %q, %q", step, i+2, argv2, warnings2, err2, argv, warnings)
					}
				}
			}
		})
	}
}

package applecontainer_test

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine/applecontainer"
	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var tstUpdate = flag.Bool("update", false, "rewrite testdata/steps.golden from the current Render output")

// tstUnknownStep satisfies the sealed plan.Step interface with a type no engine knows.
type tstUnknownStep struct{ plan.Step }

// tstWarning is an expected warning: Scope and Attribute exact, Message by prefix only.
type tstWarning struct {
	scope, attr, prefix string
}

func tstIgnored(scope, attr string) tstWarning {
	return tstWarning{scope: scope, attr: attr, prefix: "ignored ("}
}

func tstDegraded(scope, attr, value string) tstWarning {
	return tstWarning{scope: scope, attr: attr, prefix: "degraded to " + value + " ("}
}

// tstIgn is an ignored warning on the service every RunContainer case uses.
func tstIgn(attr string) tstWarning { return tstIgnored("services.web", attr) }

func tstPtr[T any](v T) *T { return &v }

func tstRender(t *testing.T, step plan.Step) ([]string, []plan.Warning) {
	t.Helper()
	argv, ws, err := applecontainer.New().Render(step)
	if err != nil {
		t.Fatalf("Render(%T): %v", step, err)
	}
	return argv, ws
}

// tstArgv compares argv with a space-separated command line; an empty line means an empty argv.
func tstArgv(t *testing.T, got []string, line string) {
	t.Helper()
	want := strings.Fields(line)
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q\n  want %q", got, want)
	}
}

// tstWarnings fails unless got holds exactly one match for every want and nothing else.
func tstWarnings(t *testing.T, got []plan.Warning, want ...tstWarning) {
	t.Helper()
	rest := slices.Clone(got)
	for _, w := range want {
		i := slices.IndexFunc(rest, func(g plan.Warning) bool {
			return g.Scope == w.scope && g.Attribute == w.attr && strings.HasPrefix(g.Message, w.prefix)
		})
		if i < 0 {
			t.Errorf("missing warning %s: %s: %s...)", w.scope, w.attr, w.prefix)
			continue
		}
		rest = slices.Delete(rest, i, i+1)
	}
	for _, g := range rest {
		t.Errorf("unexpected warning %q", g.String())
	}
}

func TestContract(t *testing.T) {
	enginetest.Contract(t, applecontainer.New())
}

func TestIdentity(t *testing.T) {
	e := applecontainer.New()
	if got := e.Name(); got != "container" {
		t.Errorf("Name() = %q, want %q", got, "container")
	}
	if got := e.Binary(); got != "container" {
		t.Errorf("Binary() = %q, want %q", got, "container")
	}
	if got, want := e.Capabilities(), (plan.Capabilities{Healthcheck: false}); got != want {
		t.Errorf("Capabilities() = %+v, want %+v", got, want)
	}
}

func TestGolden(t *testing.T) {
	e := applecontainer.New()
	var b strings.Builder
	for _, step := range enginetest.Steps() {
		argv, ws, err := e.Render(step)
		fmt.Fprintf(&b, "%s\n", reflect.TypeOf(step).Name())
		if err != nil {
			fmt.Fprintf(&b, "  error: %v\n", err)
		}
		fmt.Fprintf(&b, "  argv: %q\n", argv)
		for _, w := range ws {
			fmt.Fprintf(&b, "  warnings: %s\n", w)
		}
		b.WriteString("\n")
	}
	got := b.String()

	path := filepath.Join("testdata", "steps.golden")
	if *tstUpdate {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v; run go test ./pkg/engine/applecontainer -update to generate it", err)
	}
	if string(want) != got {
		t.Errorf("Render output differs from %s; review and rerun with -update\n%s", path, tstFirstDiff(string(want), got))
	}
}

func tstFirstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d:\n  want: %s\n  got:  %s", i+1, wl, gl)
		}
	}
	return "no differing line"
}

func TestDeterministic(t *testing.T) {
	e := applecontainer.New()
	for _, step := range enginetest.Steps() {
		argv, ws, err := e.Render(step)
		for i := 1; i < 100; i++ {
			argv2, ws2, err2 := e.Render(step)
			if !reflect.DeepEqual(argv, argv2) || !reflect.DeepEqual(ws, ws2) || (err == nil) != (err2 == nil) {
				t.Fatalf("%T: render %d gave %q %q %v, first gave %q %q %v", step, i+1, argv2, ws2, err2, argv, ws, err)
			}
		}
	}
}

type tstRunCase struct {
	name string
	edit func(*plan.ContainerSpec)
	argv string // "" means the minimal run line
	want []tstWarning
}

func tstRunCases(t *testing.T, cases []tstRunCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := plan.ContainerSpec{Service: "web", Name: "n", Image: "img"}
			if tc.edit != nil {
				tc.edit(&spec)
			}
			argv, ws := tstRender(t, plan.RunContainer{Container: spec})
			tstArgv(t, argv, cmp.Or(tc.argv, "run --detach --name n img"))
			tstWarnings(t, ws, tc.want...)
		})
	}
}

func TestRunBasics(t *testing.T) {
	tstRunCases(t, []tstRunCase{
		{name: "minimal"},
		{
			name: "entrypoint and command",
			edit: func(c *plan.ContainerSpec) {
				c.Entrypoint = []string{"e1", "e2"}
				c.Command = []string{"cmd1", "cmd2"}
			},
			argv: "run --detach --name n --entrypoint e1 img e2 cmd1 cmd2",
		},
		{
			name: "empty entrypoint",
			edit: func(c *plan.ContainerSpec) { c.Entrypoint = []string{} },
			want: []tstWarning{tstIgn("entrypoint")},
		},
		{
			name: "empty command",
			edit: func(c *plan.ContainerSpec) { c.Command = []string{} },
			want: []tstWarning{tstIgn("command")},
		},
		{
			name: "empty command with entrypoint",
			edit: func(c *plan.ContainerSpec) {
				c.Entrypoint = []string{"e1"}
				c.Command = []string{}
			},
			argv: "run --detach --name n --entrypoint e1 img",
		},
		{
			name: "platform",
			edit: func(c *plan.ContainerSpec) { c.Platform = "linux/amd64" },
			argv: "run --detach --name n --platform linux/amd64 img",
		},
		{
			name: "env",
			edit: func(c *plan.ContainerSpec) { c.Env = []plan.KV{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}} },
			argv: "run --detach --name n --env A=1 --env B=2 img",
		},
		{
			name: "labels",
			edit: func(c *plan.ContainerSpec) { c.Labels = []plan.KV{{Key: "k", Value: "v"}, {Key: "k2", Value: "v2"}} },
			argv: "run --detach --name n --label k=v --label k2=v2 img",
		},
		{
			name: "label value with equals is dropped",
			edit: func(c *plan.ContainerSpec) { c.Labels = []plan.KV{{Key: "a", Value: "b=c"}, {Key: "k", Value: "v"}} },
			argv: "run --detach --name n --label k=v img",
			want: []tstWarning{tstIgn("labels")},
		},
		{
			name: "user",
			edit: func(c *plan.ContainerSpec) { c.User = "101:101" },
			argv: "run --detach --name n --user 101:101 img",
		},
		{
			name: "workdir",
			edit: func(c *plan.ContainerSpec) { c.WorkingDir = "/w" },
			argv: "run --detach --name n --workdir /w img",
		},
		{
			name: "tty",
			edit: func(c *plan.ContainerSpec) { c.Tty = true },
			argv: "run --detach --name n --tty img",
		},
		{
			name: "stdin open",
			edit: func(c *plan.ContainerSpec) { c.StdinOpen = true },
			argv: "run --detach --name n --interactive img",
		},
		{
			name: "init true",
			edit: func(c *plan.ContainerSpec) { c.Init = tstPtr(true) },
			argv: "run --detach --name n --init img",
		},
		{
			name: "init false",
			edit: func(c *plan.ContainerSpec) { c.Init = tstPtr(false) },
		},
		{
			name: "read only",
			edit: func(c *plan.ContainerSpec) { c.ReadOnly = true },
			argv: "run --detach --name n --read-only img",
		},
		{
			name: "cap add",
			edit: func(c *plan.ContainerSpec) { c.CapAdd = []string{"NET_ADMIN", "SYS_TIME"} },
			argv: "run --detach --name n --cap-add NET_ADMIN --cap-add SYS_TIME img",
		},
		{
			name: "cap drop",
			edit: func(c *plan.ContainerSpec) { c.CapDrop = []string{"ALL"} },
			argv: "run --detach --name n --cap-drop ALL img",
		},
		{
			name: "ulimits",
			edit: func(c *plan.ContainerSpec) { c.Ulimits = []plan.Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}} },
			argv: "run --detach --name n --ulimit nofile=1024:2048 img",
		},
		{
			name: "dns",
			edit: func(c *plan.ContainerSpec) { c.DNS = []string{"1.1.1.1", "8.8.8.8"} },
			argv: "run --detach --name n --dns 1.1.1.1 --dns 8.8.8.8 img",
		},
		{
			name: "dns search",
			edit: func(c *plan.ContainerSpec) { c.DNSSearch = []string{"example.test"} },
			argv: "run --detach --name n --dns-search example.test img",
		},
		{
			name: "dns options",
			edit: func(c *plan.ContainerSpec) { c.DNSOpts = []string{"ndots:2"} },
			argv: "run --detach --name n --dns-option ndots:2 img",
		},
		{
			name: "tmpfs",
			edit: func(c *plan.ContainerSpec) { c.Tmpfs = []string{"/run", "/tmp"} },
			argv: "run --detach --name n --tmpfs /run --tmpfs /tmp img",
		},
		{
			name: "shm size",
			edit: func(c *plan.ContainerSpec) { c.ShmSize = 64 << 20 },
			argv: "run --detach --name n --shm-size 64M img",
		},
	})
}

func TestRunCommandUnquoted(t *testing.T) {
	spec := plan.ContainerSpec{Service: "web", Name: "n", Image: "img", Command: []string{"sh", "-c", "echo hi"}}
	argv, ws := tstRender(t, plan.RunContainer{Container: spec})
	want := []string{"run", "--detach", "--name", "n", "img", "sh", "-c", "echo hi"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %q\n  want %q", argv, want)
	}
	tstWarnings(t, ws)
}

func TestRunResources(t *testing.T) {
	tstRunCases(t, []tstRunCase{
		{
			name: "memory whole MiB",
			edit: func(c *plan.ContainerSpec) { c.Memory = 512 << 20 },
			argv: "run --detach --name n --memory 512M img",
		},
		{
			name: "memory rounds up",
			edit: func(c *plan.ContainerSpec) { c.Memory = 512<<20 + 1 },
			argv: "run --detach --name n --memory 513M img",
			want: []tstWarning{tstDegraded("services.web", "memory", "513M")},
		},
		{
			name: "cpus fractional",
			edit: func(c *plan.ContainerSpec) { c.CPUs = 1.5 },
			argv: "run --detach --name n --cpus 2 img",
			want: []tstWarning{tstDegraded("services.web", "cpus", "2")},
		},
		{
			name: "cpus whole",
			edit: func(c *plan.ContainerSpec) { c.CPUs = 2 },
			argv: "run --detach --name n --cpus 2 img",
		},
	})
}

func TestRunPorts(t *testing.T) {
	tstRunCases(t, []tstRunCase{
		{
			name: "host ip and protocol",
			edit: func(c *plan.ContainerSpec) {
				c.Ports = []plan.Port{{HostIP: "127.0.0.1", Published: "8080", Target: 80, Protocol: "tcp"}}
			},
			argv: "run --detach --name n --publish 127.0.0.1:8080:80/tcp img",
		},
		{
			name: "bare",
			edit: func(c *plan.ContainerSpec) { c.Ports = []plan.Port{{Published: "8080", Target: 80}} },
			argv: "run --detach --name n --publish 8080:80 img",
		},
		{
			name: "udp",
			edit: func(c *plan.ContainerSpec) { c.Ports = []plan.Port{{Published: "53", Target: 53, Protocol: "udp"}} },
			argv: "run --detach --name n --publish 53:53/udp img",
		},
		{
			name: "unpublished",
			edit: func(c *plan.ContainerSpec) { c.Ports = []plan.Port{{Target: 80, Protocol: "tcp"}} },
			want: []tstWarning{tstIgn("ports")},
		},
		{
			name: "range degrades to first port",
			edit: func(c *plan.ContainerSpec) {
				c.Ports = []plan.Port{{Published: "8000-8001", Target: 8000, Protocol: "tcp"}}
			},
			argv: "run --detach --name n --publish 8000:8000/tcp img",
			want: []tstWarning{tstDegraded("services.web", "ports", "8000")},
		},
		{
			name: "sctp",
			edit: func(c *plan.ContainerSpec) { c.Ports = []plan.Port{{Published: "8080", Target: 80, Protocol: "sctp"}} },
			want: []tstWarning{tstIgn("ports")},
		},
		{
			name: "host mode",
			edit: func(c *plan.ContainerSpec) {
				c.Ports = []plan.Port{{Mode: "host", Published: "8080", Target: 80, Protocol: "tcp"}}
			},
			argv: "run --detach --name n --publish 8080:80/tcp img",
			want: []tstWarning{tstIgn("ports.mode")},
		},
		{
			name: "ingress mode",
			edit: func(c *plan.ContainerSpec) {
				c.Ports = []plan.Port{{Mode: "ingress", Published: "8080", Target: 80, Protocol: "tcp"}}
			},
			argv: "run --detach --name n --publish 8080:80/tcp img",
		},
	})
}

func TestRunMounts(t *testing.T) {
	tstRunCases(t, []tstRunCase{
		{
			name: "bind",
			edit: func(c *plan.ContainerSpec) { c.Mounts = []plan.Mount{{Type: "bind", Source: "/src", Target: "/dst"}} },
			argv: "run --detach --name n --mount type=bind,source=/src,target=/dst img",
		},
		{
			name: "bind read only",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/src", Target: "/dst", ReadOnly: true}}
			},
			argv: "run --detach --name n --mount type=bind,source=/src,target=/dst,readonly img",
		},
		{
			name: "volume",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "volume", Source: "proj_data", Target: "/data"}}
			},
			argv: "run --detach --name n --mount type=volume,source=proj_data,target=/data img",
		},
		{
			name: "volume read only",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "volume", Source: "proj_data", Target: "/data", ReadOnly: true}}
			},
			argv: "run --detach --name n --mount type=volume,source=proj_data,target=/data,readonly img",
		},
		{
			name: "anonymous volume",
			edit: func(c *plan.ContainerSpec) { c.Mounts = []plan.Mount{{Type: "volume", Target: "/data"}} },
			argv: "run --detach --name n --mount type=volume,target=/data img",
		},
		{
			name: "tmpfs with options",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "tmpfs", Target: "/run", Tmpfs: &plan.TmpfsOptions{Size: 64 << 20, Mode: 0o1777}}}
			},
			argv: "run --detach --name n --mount type=tmpfs,target=/run,size=64M,mode=1777 img",
		},
		{
			name: "tmpfs bare",
			edit: func(c *plan.ContainerSpec) { c.Mounts = []plan.Mount{{Type: "tmpfs", Target: "/run"}} },
			argv: "run --detach --name n --mount type=tmpfs,target=/run img",
		},
		{
			name: "image type",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "image", Source: "nginx:1.27", Target: "/x", Image: &plan.ImageOptions{Subpath: "usr/share"}}}
			},
			want: []tstWarning{tstIgn("volumes")},
		},
		{
			name: "bind options",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/src", Target: "/dst", Bind: &plan.BindOptions{
					Propagation: "rprivate", SELinux: "z", CreateHostPath: true, Recursive: "enabled",
				}}}
			},
			argv: "run --detach --name n --mount type=bind,source=/src,target=/dst img",
			want: []tstWarning{tstIgn("volumes.bind.propagation"), tstIgn("volumes.bind.selinux"), tstIgn("volumes.bind.recursive")},
		},
		{
			name: "volume options",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "volume", Source: "proj_data", Target: "/data", Volume: &plan.VolumeOptions{
					NoCopy: true, Subpath: "html", Labels: []plan.KV{{Key: "k", Value: "v"}},
				}}}
			},
			argv: "run --detach --name n --mount type=volume,source=proj_data,target=/data img",
			want: []tstWarning{tstIgn("volumes.volume.nocopy"), tstIgn("volumes.volume.subpath"), tstIgn("volumes.volume.labels")},
		},
		{
			name: "consistency",
			edit: func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/src", Target: "/dst", Consistency: "cached"}}
			},
			argv: "run --detach --name n --mount type=bind,source=/src,target=/dst img",
			want: []tstWarning{tstIgn("volumes.consistency")},
		},
	})
}

func TestRunNetworks(t *testing.T) {
	tstRunCases(t, []tstRunCase{
		{
			name: "aliases and container mac",
			edit: func(c *plan.ContainerSpec) {
				c.MacAddress = "02:42:ac:1c:00:0a"
				c.Networks = []plan.NetworkAttachment{
					{Key: "front", Name: "proj_front", Aliases: []string{"web", "a"}},
					{Key: "back", Name: "proj_back", Aliases: []string{"web"}},
				}
			},
			argv: "run --detach --name n --network proj_front,alias=web,alias=a,mac=02:42:ac:1c:00:0a --network proj_back,alias=web img",
		},
		{
			name: "attachment mac",
			edit: func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", MacAddress: "02:42:ac:1c:00:0b"}}
			},
			argv: "run --detach --name n --network proj_front,mac=02:42:ac:1c:00:0b img",
		},
		{
			name: "container mac goes to first attachment without one",
			edit: func(c *plan.ContainerSpec) {
				c.MacAddress = "02:42:ac:1d:00:0a"
				c.Networks = []plan.NetworkAttachment{
					{Key: "front", Name: "proj_front", MacAddress: "02:42:ac:1c:00:0b"},
					{Key: "back", Name: "proj_back"},
				}
			},
			argv: "run --detach --name n --network proj_front,mac=02:42:ac:1c:00:0b --network proj_back,mac=02:42:ac:1d:00:0a img",
		},
		{
			name: "container mac without attachments",
			edit: func(c *plan.ContainerSpec) { c.MacAddress = "02:42:ac:1c:00:0a" },
			want: []tstWarning{tstIgn("mac_address")},
		},
		{
			name: "ipv4 address",
			edit: func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", IPv4: "172.28.0.10"}}
			},
			argv: "run --detach --name n --network proj_front img",
			want: []tstWarning{tstIgn("networks.front.ipv4_address")},
		},
		{
			name: "other attachment options",
			edit: func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{
					Key: "front", Name: "proj_front",
					IPv6:          "fd00:28::10",
					LinkLocalIPs:  []string{"169.254.28.10"},
					DriverOpts:    []plan.KV{{Key: "com.docker.network.endpoint.ifname", Value: "eth0"}},
					Priority:      100,
					GwPriority:    1,
					InterfaceName: "eth0",
				}}
			},
			argv: "run --detach --name n --network proj_front img",
			want: []tstWarning{
				tstIgn("networks.front.ipv6_address"),
				tstIgn("networks.front.link_local_ips"),
				tstIgn("networks.front.driver_opts"),
				tstIgn("networks.front.priority"),
				tstIgn("networks.front.gw_priority"),
				tstIgn("networks.front.interface_name"),
			},
		},
		{
			name: "network mode host keeps attachments",
			edit: func(c *plan.ContainerSpec) {
				c.NetworkMode = "host"
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front"}}
			},
			argv: "run --detach --name n --network proj_front img",
			want: []tstWarning{tstIgn("network_mode")},
		},
	})
}

func TestRunIgnoredAttributes(t *testing.T) {
	cases := []struct {
		name string
		attr string
		edit func(*plan.ContainerSpec)
	}{
		{"hostname", "hostname", func(c *plan.ContainerSpec) { c.Hostname = "web" }},
		{"domainname", "domainname", func(c *plan.ContainerSpec) { c.DomainName = "example.test" }},
		{"extra_hosts", "extra_hosts", func(c *plan.ContainerSpec) { c.ExtraHosts = []string{"db:10.0.0.5"} }},
		{"links", "links", func(c *plan.ContainerSpec) { c.Links = []string{"proj-db-1:db"} }},
		{"expose", "expose", func(c *plan.ContainerSpec) { c.Expose = []string{"8081"} }},
		{"pull_policy always", "pull_policy", func(c *plan.ContainerSpec) { c.PullPolicy = "always" }},
		{"pull_policy never", "pull_policy", func(c *plan.ContainerSpec) { c.PullPolicy = "never" }},
		{"restart", "restart", func(c *plan.ContainerSpec) { c.Restart = "always" }},
		{"stop_signal", "stop_signal", func(c *plan.ContainerSpec) { c.StopSignal = "SIGQUIT" }},
		{"healthcheck", "healthcheck", func(c *plan.ContainerSpec) {
			c.Healthcheck = &plan.Healthcheck{Test: []string{"CMD", "true"}, Interval: time.Second}
		}},
		{"logging", "logging", func(c *plan.ContainerSpec) { c.Logging = &plan.Logging{Driver: "json-file"} }},
		{"annotations", "annotations", func(c *plan.ContainerSpec) { c.Annotations = []plan.KV{{Key: "a", Value: "b"}} }},
		{"privileged", "privileged", func(c *plan.ContainerSpec) { c.Privileged = true }},
		{"security_opt", "security_opt", func(c *plan.ContainerSpec) { c.SecurityOpt = []string{"no-new-privileges:true"} }},
		{"group_add", "group_add", func(c *plan.ContainerSpec) { c.GroupAdd = []string{"audio"} }},
		{"device_cgroup_rules", "device_cgroup_rules", func(c *plan.ContainerSpec) { c.DeviceCgroupRules = []string{"c 10:229 rwm"} }},
		{"devices", "devices", func(c *plan.ContainerSpec) { c.Devices = []plan.Device{{Source: "/dev/fuse", Target: "/dev/fuse"}} }},
		{"gpus", "gpus", func(c *plan.ContainerSpec) { c.Gpus = []plan.DeviceRequest{{Driver: "nvidia", Count: 1}} }},
		{"sysctls", "sysctls", func(c *plan.ContainerSpec) { c.Sysctls = []plan.KV{{Key: "net.core.somaxconn", Value: "1024"}} }},
		{"pid", "pid", func(c *plan.ContainerSpec) { c.Pid = "host" }},
		{"ipc", "ipc", func(c *plan.ContainerSpec) { c.Ipc = "shareable" }},
		{"uts", "uts", func(c *plan.ContainerSpec) { c.Uts = "host" }},
		{"userns_mode", "userns_mode", func(c *plan.ContainerSpec) { c.Userns = "host" }},
		{"cgroup_parent", "cgroup_parent", func(c *plan.ContainerSpec) { c.CgroupParent = "/compose" }},
		{"cgroup", "cgroup", func(c *plan.ContainerSpec) { c.Cgroup = "private" }},
		{"runtime", "runtime", func(c *plan.ContainerSpec) { c.Runtime = "runc" }},
		{"isolation", "isolation", func(c *plan.ContainerSpec) { c.Isolation = "hyperv" }},
		{"cpu_shares", "cpu_shares", func(c *plan.ContainerSpec) { c.CPUShares = 512 }},
		{"cpuset", "cpuset", func(c *plan.ContainerSpec) { c.CPUSet = "0-1" }},
		{"cpu_period", "cpu_period", func(c *plan.ContainerSpec) { c.CPUPeriod = 100000 }},
		{"cpu_quota", "cpu_quota", func(c *plan.ContainerSpec) { c.CPUQuota = 150000 }},
		{"cpu_rt_period", "cpu_rt_period", func(c *plan.ContainerSpec) { c.CPURTPeriod = 1000000 }},
		{"cpu_rt_runtime", "cpu_rt_runtime", func(c *plan.ContainerSpec) { c.CPURTRuntime = 950000 }},
		{"cpu_count", "cpu_count", func(c *plan.ContainerSpec) { c.CPUCount = 2 }},
		{"cpu_percent", "cpu_percent", func(c *plan.ContainerSpec) { c.CPUPercent = 50 }},
		{"mem_reservation", "mem_reservation", func(c *plan.ContainerSpec) { c.MemoryReservation = 256 << 20 }},
		{"memswap_limit", "memswap_limit", func(c *plan.ContainerSpec) { c.MemorySwap = 1 << 30 }},
		{"mem_swappiness", "mem_swappiness", func(c *plan.ContainerSpec) { c.MemorySwappiness = 60 }},
		{"oom_kill_disable", "oom_kill_disable", func(c *plan.ContainerSpec) { c.OomKillDisable = true }},
		{"oom_score_adj", "oom_score_adj", func(c *plan.ContainerSpec) { c.OomScoreAdj = 100 }},
		{"pids_limit", "pids_limit", func(c *plan.ContainerSpec) { c.PidsLimit = 100 }},
		{"blkio_config", "blkio_config", func(c *plan.ContainerSpec) { c.Blkio = &plan.Blkio{Weight: 500} }},
		{"volumes_from", "volumes_from", func(c *plan.ContainerSpec) { c.VolumesFrom = []string{"proj-data-1"} }},
		{"volume_driver", "volume_driver", func(c *plan.ContainerSpec) { c.VolumeDriver = "local" }},
		{"storage_opt", "storage_opt", func(c *plan.ContainerSpec) { c.StorageOpt = []plan.KV{{Key: "size", Value: "10G"}} }},
	}
	var runs []tstRunCase
	for _, tc := range cases {
		runs = append(runs, tstRunCase{name: tc.name, edit: tc.edit, want: []tstWarning{tstIgn(tc.attr)}})
	}
	tstRunCases(t, runs)
}

func TestRunSilentAttributes(t *testing.T) {
	tstRunCases(t, []tstRunCase{
		{name: "pull policy missing", edit: func(c *plan.ContainerSpec) { c.PullPolicy = "missing" }},
		{name: "restart no", edit: func(c *plan.ContainerSpec) { c.Restart = "no" }},
		{name: "stop timeout", edit: func(c *plan.ContainerSpec) { c.StopTimeout = tstPtr(20 * time.Second) }},
		{name: "healthcheck disabled", edit: func(c *plan.ContainerSpec) { c.Healthcheck = &plan.Healthcheck{Disable: true} }},
		{name: "number", edit: func(c *plan.ContainerSpec) { c.Number = 3 }},
	})
}

func TestOtherSteps(t *testing.T) {
	cases := []struct {
		name string
		step plan.Step
		argv string
		want []tstWarning
	}{
		{
			name: "network v4 and v6 pools",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", IPAM: []plan.IPAMPool{{Subnet: "172.28.0.0/16"}, {Subnet: "fd00:28::/64"}}}},
			argv: "network create --subnet 172.28.0.0/16 --subnet-v6 fd00:28::/64 proj_front",
		},
		{
			name: "network internal with labels",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", Internal: true, Labels: []plan.KV{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}}},
			argv: "network create --internal --label a=1 --label b=2 proj_front",
		},
		{
			name: "network second v4 pool ignored",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", IPAM: []plan.IPAMPool{{Subnet: "172.28.0.0/16"}, {Subnet: "fd00:28::/64"}, {Subnet: "172.29.0.0/16"}}}},
			argv: "network create --subnet 172.28.0.0/16 --subnet-v6 fd00:28::/64 proj_front",
			want: []tstWarning{tstIgnored("networks.proj_front", "ipam.config")},
		},
		{
			name: "network gateway",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", IPAM: []plan.IPAMPool{{Subnet: "172.28.0.0/16", Gateway: "172.28.0.1"}}}},
			argv: "network create --subnet 172.28.0.0/16 proj_front",
			want: []tstWarning{tstIgnored("networks.proj_front", "ipam.config.gateway")},
		},
		{
			name: "network ip range and aux addresses",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", IPAM: []plan.IPAMPool{{
				Subnet: "172.28.0.0/16", IPRange: "172.28.5.0/24", AuxAddresses: []plan.KV{{Key: "router", Value: "172.28.1.5"}},
			}}}},
			argv: "network create --subnet 172.28.0.0/16 proj_front",
			want: []tstWarning{tstIgnored("networks.proj_front", "ipam.config.ip_range"), tstIgnored("networks.proj_front", "ipam.config.aux_addresses")},
		},
		{
			name: "network bridge driver and default ipam driver",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", Driver: "bridge", IPAMDriver: "default", EnableIPv4: tstPtr(true)}},
			argv: "network create proj_front",
		},
		{
			name: "network unsupported drivers and options",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{
				Name: "proj_front", Driver: "overlay", DriverOpts: []plan.KV{{Key: "k", Value: "v"}},
				IPAMDriver: "custom", IPAMOptions: []plan.KV{{Key: "k", Value: "v"}}, Attachable: true,
			}},
			argv: "network create proj_front",
			want: []tstWarning{
				tstIgnored("networks.proj_front", "driver"),
				tstIgnored("networks.proj_front", "driver_opts"),
				tstIgnored("networks.proj_front", "ipam.driver"),
				tstIgnored("networks.proj_front", "ipam.options"),
				tstIgnored("networks.proj_front", "attachable"),
			},
		},
		{
			name: "network enable ipv4 false",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", EnableIPv4: tstPtr(false)}},
			argv: "network create proj_front",
			want: []tstWarning{tstIgnored("networks.proj_front", "enable_ipv4")},
		},
		{
			name: "network enable ipv6 without pool",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", EnableIPv6: tstPtr(true)}},
			argv: "network create proj_front",
			want: []tstWarning{tstIgnored("networks.proj_front", "enable_ipv6")},
		},
		{
			name: "network enable ipv6 with pool",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_front", EnableIPv6: tstPtr(true), IPAM: []plan.IPAMPool{{Subnet: "fd00:28::/64"}}}},
			argv: "network create --subnet-v6 fd00:28::/64 proj_front",
		},
		{
			name: "volume size and journal pass through",
			step: plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data", Driver: "local", DriverOpts: []plan.KV{{Key: "journal", Value: "false"}, {Key: "size", Value: "1G"}}}},
			argv: "volume create --opt journal=false --opt size=1G proj_data",
		},
		{
			name: "volume labels",
			step: plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data", Labels: []plan.KV{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}}},
			argv: "volume create --label a=1 --label b=2 proj_data",
		},
		{
			name: "volume unknown driver option",
			step: plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data", DriverOpts: []plan.KV{{Key: "foo", Value: "bar"}, {Key: "size", Value: "1G"}}}},
			argv: "volume create --opt size=1G proj_data",
			want: []tstWarning{tstIgnored("volumes.proj_data", "driver_opts.foo")},
		},
		{
			name: "volume non-local driver",
			step: plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data", Driver: "nfs"}},
			argv: "volume create proj_data",
			want: []tstWarning{tstIgnored("volumes.proj_data", "driver")},
		},
		{
			name: "build all fields",
			step: plan.BuildImage{Build: plan.BuildSpec{
				Service:            "web",
				Tag:                "proj-web",
				Context:            "/srv/web",
				Dockerfile:         "Dockerfile.prod",
				Args:               []plan.KV{{Key: "A", Value: "1"}},
				Target:             "runtime",
				Labels:             []plan.KV{{Key: "k", Value: "v"}},
				NoCache:            true,
				Pull:               true,
				Platforms:          []string{"linux/amd64", "linux/arm64"},
				CacheFrom:          []string{"type=registry,ref=registry.example.test/proj-web:cache"},
				CacheTo:            []string{"type=inline"},
				ExtraHosts:         []string{"db:10.0.0.5"},
				Network:            "host",
				ShmSize:            64 << 20,
				SSH:                []string{"default"},
				Secrets:            []plan.BuildSecret{{ID: "tok", Source: "/s"}},
				AdditionalContexts: []plan.KV{{Key: "assets", Value: "/srv/assets"}},
				Tags:               []string{"registry.example.test/proj-web:1.0"},
				Ulimits:            []plan.Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}},
				Isolation:          "hyperv",
				Privileged:         true,
			}},
			argv: "build --file Dockerfile.prod --build-arg A=1 --target runtime --label k=v --no-cache --pull --platform linux/amd64 --platform linux/arm64 --ssh default --secret id=tok,src=/s --tag proj-web --tag registry.example.test/proj-web:1.0 /srv/web",
			want: []tstWarning{
				tstIgnored("services.web", "build.cache_from"),
				tstIgnored("services.web", "build.cache_to"),
				tstIgnored("services.web", "build.extra_hosts"),
				tstIgnored("services.web", "build.network"),
				tstIgnored("services.web", "build.shm_size"),
				tstIgnored("services.web", "build.additional_contexts"),
				tstIgnored("services.web", "build.ulimits"),
				tstIgnored("services.web", "build.isolation"),
				tstIgnored("services.web", "build.privileged"),
			},
		},
		{
			name: "build secret without source",
			step: plan.BuildImage{Build: plan.BuildSpec{Service: "web", Tag: "proj-web", Context: ".", Secrets: []plan.BuildSecret{{ID: "tok"}}}},
			argv: "build --secret id=tok --tag proj-web .",
		},
		{
			name: "build non-default ssh entry",
			step: plan.BuildImage{Build: plan.BuildSpec{Service: "api", Tag: "proj-api", Context: ".", SSH: []string{"default", "agent=/run/ssh.sock"}}},
			argv: "build --ssh default --tag proj-api .",
			want: []tstWarning{tstIgnored("services.api", "build.ssh")},
		},
		{
			name: "start one",
			step: plan.StartContainers{Names: []string{"proj-web-1"}},
			argv: "start proj-web-1",
		},
		{
			name: "stop without timeout",
			step: plan.StopContainers{Names: []string{"a", "b"}},
			argv: "stop a b",
		},
		{
			name: "stop rounds timeout up",
			step: plan.StopContainers{Names: []string{"a"}, Timeout: tstPtr(1500 * time.Millisecond)},
			argv: "stop --time 2 a",
		},
		{
			name: "stop whole seconds",
			step: plan.StopContainers{Names: []string{"a"}, Timeout: tstPtr(10 * time.Second)},
			argv: "stop --time 10 a",
		},
		{
			name: "remove containers",
			step: plan.RemoveContainers{Names: []string{"a", "b"}},
			argv: "delete --force a b",
		},
		{
			name: "remove containers with volumes",
			step: plan.RemoveContainers{Names: []string{"a", "b"}, Volumes: true},
			argv: "delete --force a b",
			want: []tstWarning{tstIgnored("containers.a", "volumes"), tstIgnored("containers.b", "volumes")},
		},
		{name: "remove network", step: plan.RemoveNetwork{Name: "proj_front"}, argv: "network delete proj_front"},
		{name: "remove volume", step: plan.RemoveVolume{Name: "proj_data"}, argv: "volume delete proj_data"},
		{name: "wait completed", step: plan.WaitContainer{Name: "proj-web-1", Condition: plan.ConditionCompleted}, argv: ""},
		{name: "list containers", step: plan.ListContainers{Project: "proj"}, argv: "list --all --format json"},
		{name: "list networks", step: plan.ListNetworks{}, argv: "network list --format json"},
		{name: "list volumes", step: plan.ListVolumes{}, argv: "volume list --format json"},
		{name: "list images", step: plan.ListImages{}, argv: "image list --format json"},
		{name: "inspect", step: plan.InspectContainer{Name: "proj-web-1"}, argv: "inspect proj-web-1"},
		{name: "logs follow tail", step: plan.Logs{Name: "proj-web-1", Follow: true, Tail: "10"}, argv: "logs --follow -n 10 proj-web-1"},
		{name: "logs tail only", step: plan.Logs{Name: "proj-web-1", Tail: "10"}, argv: "logs -n 10 proj-web-1"},
		{name: "logs empty tail", step: plan.Logs{Name: "proj-web-1"}, argv: "logs proj-web-1"},
		{name: "logs tail all", step: plan.Logs{Name: "proj-web-1", Follow: true, Tail: "all"}, argv: "logs --follow proj-web-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv, ws := tstRender(t, tc.step)
			tstArgv(t, argv, tc.argv)
			tstWarnings(t, ws, tc.want...)
		})
	}
}

func TestRenderErrors(t *testing.T) {
	cases := []struct {
		name string
		step plan.Step
		want string // substring of the error message; "" accepts any error
	}{
		{"run without name", plan.RunContainer{Container: plan.ContainerSpec{Service: "web", Image: "img"}}, ""},
		{"run without image", plan.RunContainer{Container: plan.ContainerSpec{Service: "web", Name: "n"}}, ""},
		{"build without tag", plan.BuildImage{Build: plan.BuildSpec{Service: "web", Context: "/srv/web"}}, "empty Tag"},
		{"build without context", plan.BuildImage{Build: plan.BuildSpec{Service: "web", Tag: "proj-web"}}, "empty Context"},
		{"start without names", plan.StartContainers{}, ""},
		{"start two names", plan.StartContainers{Names: []string{"a", "b"}}, ""},
		{"stop without names", plan.StopContainers{}, ""},
		{"remove containers without names", plan.RemoveContainers{}, ""},
		{"remove network without name", plan.RemoveNetwork{}, ""},
		{"remove volume without name", plan.RemoveVolume{}, ""},
		{"wait without name", plan.WaitContainer{Condition: plan.ConditionCompleted}, ""},
		{"wait healthy", plan.WaitContainer{Name: "proj-web-1", Condition: plan.ConditionHealthy}, ""},
		{"inspect without name", plan.InspectContainer{}, ""},
		{"logs without name", plan.Logs{}, ""},
		{"nil step", nil, ""},
		{"unknown step", tstUnknownStep{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			argv, ws, err := applecontainer.New().Render(tc.step)
			if err == nil {
				t.Fatalf("Render(%T) = %q, nil error; want an error", tc.step, argv)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Render(%T) error = %q; want it to contain %q", tc.step, err, tc.want)
			}
			if argv != nil {
				t.Errorf("Render(%T) argv = %q with error %v; want nil argv", tc.step, argv, err)
			}
			if ws != nil {
				t.Errorf("Render(%T) warnings = %q with error %v; want nil warnings", tc.step, ws, err)
			}
		})
	}
}

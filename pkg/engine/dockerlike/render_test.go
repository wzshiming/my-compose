package dockerlike_test

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

	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var tstUpdate = flag.Bool("update", false, "rewrite testdata/<variant>/steps.golden from the current Render output")

const tstWeb = "services.web"

// tstWarn is an expected warning: exact Scope and Attribute, but only a Message prefix since the parenthesised reason is free text.
type tstWarn struct {
	Scope, Attribute, Prefix string
}

func (w tstWarn) String() string { return w.Scope + ": " + w.Attribute + ": " + w.Prefix + "..." }

func tstIgnored(scope, attribute string) tstWarn {
	return tstWarn{Scope: scope, Attribute: attribute, Prefix: "ignored ("}
}

func tstDegraded(scope, attribute, value string) tstWarn {
	return tstWarn{Scope: scope, Attribute: attribute, Prefix: "degraded to " + value + " ("}
}

type tstRenderCase struct {
	name    string
	variant string
	step    plan.Step
	argv    []string
	warns   []tstWarn
}

// tstRender renders every case and checks its exact argv and its warnings.
func tstRender(t *testing.T, cases []tstRenderCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.variant+"/"+tt.name, func(t *testing.T) {
			argv, warnings, err := tstNew(t, tt.variant).Render(tt.step)
			if err != nil {
				t.Fatalf("Render(%T) error: %v", tt.step, err)
			}
			if !reflect.DeepEqual(argv, tt.argv) {
				t.Errorf("argv =\n  %q\nwant\n  %q", argv, tt.argv)
			}
			tstCheckWarnings(t, warnings, tt.warns)
		})
	}
}

func tstCheckWarnings(t *testing.T, got []plan.Warning, want []tstWarn) {
	t.Helper()
	if len(got) > 0 && !reflect.DeepEqual(got, plan.NormalizeWarnings(got)) {
		t.Errorf("warnings are not normalized: %q", got)
	}
	want = slices.Clone(want)
	slices.SortFunc(want, func(a, b tstWarn) int {
		return cmp.Or(cmp.Compare(a.Scope, b.Scope), cmp.Compare(a.Attribute, b.Attribute), cmp.Compare(a.Prefix, b.Prefix))
	})
	if len(got) != len(want) {
		t.Errorf("warnings =\n  %q\nwant %d:\n  %v", got, len(want), want)
		return
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Scope != w.Scope || g.Attribute != w.Attribute || !strings.HasPrefix(g.Message, w.Prefix) {
			t.Errorf("warning[%d] = %q, want %v", i, g, w)
		}
	}
}

// tstRunStep returns the minimal RunContainer step (service web, container n, image img) with f applied.
func tstRunStep(f func(c *plan.ContainerSpec)) plan.RunContainer {
	c := plan.ContainerSpec{Service: "web", Number: 1, Name: "n", Image: "img"}
	if f != nil {
		f(&c)
	}
	return plan.RunContainer{Container: c}
}

// tstRunArgv builds run -d --name n <flags> img <args>.
func tstRunArgv(flags []string, args ...string) []string {
	argv := append([]string{"run", "-d", "--name", "n"}, flags...)
	argv = append(argv, "img")
	return append(argv, args...)
}

func tstPtr[T any](v T) *T { return &v }

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
			return fmt.Sprintf("line %d:\n  golden: %q\n  render: %q", i+1, wl, gl)
		}
	}
	return "no differing line (trailing whitespace?)"
}

func TestGolden(t *testing.T) {
	for _, v := range tstVariants {
		t.Run(v, func(t *testing.T) {
			e := tstNew(t, v)
			var b strings.Builder
			for _, step := range enginetest.Steps() {
				argv, warnings, err := e.Render(step)
				if err != nil {
					t.Fatalf("Render(%T) error: %v", step, err)
				}
				fmt.Fprintf(&b, "%s\n  argv: %q\n  warnings:", reflect.TypeOf(step).Name(), argv)
				for _, w := range warnings {
					fmt.Fprintf(&b, "\n    %s", w)
				}
				b.WriteString("\n")
			}
			got := b.String()
			path := filepath.Join("testdata", v, "steps.golden")
			if *tstUpdate {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v; run go test ./pkg/engine/dockerlike -update to create it", err)
			}
			if string(want) != got {
				t.Errorf("%s differs from Render output; run go test ./pkg/engine/dockerlike -update after reviewing:\n%s", path, tstFirstDiff(string(want), got))
			}
		})
	}
}

func TestRunMinimal(t *testing.T) {
	var cases []tstRenderCase
	for _, v := range tstVariants {
		cases = append(cases,
			tstRenderCase{name: "name and image only", variant: v, step: tstRunStep(nil), argv: tstRunArgv(nil)},
			tstRenderCase{name: "silent zero values", variant: v, step: tstRunStep(func(c *plan.ContainerSpec) {
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
			name: "entrypoint rest without command degrades", variant: "docker",
			step:  tstRunStep(func(c *plan.ContainerSpec) { c.Entrypoint = []string{"/bin/sh", "-c"} }),
			argv:  tstRunArgv([]string{"--entrypoint", "/bin/sh"}, "-c"),
			warns: []tstWarn{tstDegraded(tstWeb, "entrypoint", "/bin/sh")},
		},
		{
			name: "entrypoint rest precedes command", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Entrypoint = []string{"/bin/sh", "-c"}
				c.Command = []string{"echo hi"}
			}),
			argv: tstRunArgv([]string{"--entrypoint", "/bin/sh"}, "-c", "echo hi"),
		},
		{
			name: "single entrypoint and command", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Entrypoint = []string{"/entry.sh"}
				c.Command = []string{"serve", "--port", "80"}
			}),
			argv: tstRunArgv([]string{"--entrypoint", "/entry.sh"}, "serve", "--port", "80"),
		},
		{
			name: "command only", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Command = []string{"nginx", "-g", "daemon off;"} }),
			argv: tstRunArgv(nil, "nginx", "-g", "daemon off;"),
		},
		{
			name: "empty entrypoint clears the image default", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Entrypoint = []string{} }),
			argv: tstRunArgv([]string{"--entrypoint", ""}),
		},
		{
			name: "empty command is ignored", variant: "docker",
			step:  tstRunStep(func(c *plan.ContainerSpec) { c.Command = []string{} }),
			argv:  tstRunArgv(nil),
			warns: []tstWarn{tstIgnored(tstWeb, "command")},
		},
	})
}

func TestRunBasics(t *testing.T) {
	tstRender(t, []tstRenderCase{
		{
			name: "pull always", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.PullPolicy = "always" }),
			argv: tstRunArgv([]string{"--pull", "always"}),
		},
		{
			name: "pull never", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.PullPolicy = "never" }),
			argv: tstRunArgv([]string{"--pull", "never"}),
		},
		{
			name: "pull missing is the default", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.PullPolicy = "missing" }),
			argv: tstRunArgv(nil),
		},
		{
			name: "platform", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Platform = "linux/amd64" }),
			argv: tstRunArgv([]string{"--platform", "linux/amd64"}),
		},
		{
			name: "labels then annotations", variant: "docker",
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
			name: "env", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Env = []plan.KV{{Key: "A", Value: "1"}, {Key: "B", Value: "x=y"}}
			}),
			argv: tstRunArgv([]string{"--env", "A=1", "--env", "B=x=y"}),
		},
		{
			name: "workdir and user", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.WorkingDir = "/srv"
				c.User = "101:101"
			}),
			argv: tstRunArgv([]string{"--workdir", "/srv", "--user", "101:101"}),
		},
		{
			name: "tty and stdin", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Tty = true
				c.StdinOpen = true
			}),
			argv: tstRunArgv([]string{"-t", "-i"}),
		},
		{
			name: "init nil", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Init = nil }),
			argv: tstRunArgv(nil),
		},
		{
			name: "init true", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Init = tstPtr(true) }),
			argv: tstRunArgv([]string{"--init"}),
		},
		{
			name: "init false", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Init = tstPtr(false) }),
			argv: tstRunArgv(nil),
		},
		{
			name: "stop signal and timeout rounded up", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.StopSignal = "SIGQUIT"
				c.StopTimeout = tstPtr(1500 * time.Millisecond)
			}),
			argv: tstRunArgv([]string{"--stop-signal", "SIGQUIT", "--stop-timeout", "2"}),
		},
		{
			name: "restart on-failure", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Restart = "on-failure:3" }),
			argv: tstRunArgv([]string{"--restart", "on-failure:3"}),
		},
		{
			name: "hostname domainname and dns", variant: "docker",
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
			name: "expose and links", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Expose = []string{"8081"}
				c.Links = []string{"proj-db-1:db"}
			}),
			argv: tstRunArgv([]string{"--expose", "8081", "--link", "proj-db-1:db"}),
		},
		{
			name: "security flags", variant: "docker",
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
			name: "namespaces and runtime", variant: "docker",
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
			name: "logging", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Logging = &plan.Logging{Driver: "json-file", Options: []plan.KV{{Key: "max-file", Value: "3"}, {Key: "max-size", Value: "10m"}}}
			}),
			argv: tstRunArgv([]string{"--log-driver", "json-file", "--log-opt", "max-file=3", "--log-opt", "max-size=10m"}),
		},
	})
}

func TestRunPorts(t *testing.T) {
	ports := func(c *plan.ContainerSpec) {
		c.Ports = []plan.Port{
			{Target: 80},
			{Published: "8080", Target: 80, Protocol: "tcp"},
			{HostIP: "127.0.0.1", Published: "8080", Target: 80},
			{HostIP: "127.0.0.1", Target: 80, Protocol: "udp"},
			{Published: "8000-8010", Target: 8000},
		}
	}
	want := tstRunArgv([]string{
		"-p", "80",
		"-p", "8080:80/tcp",
		"-p", "127.0.0.1:8080:80",
		"-p", "127.0.0.1::80/udp",
		"-p", "8000-8010:8000",
	})
	tstRender(t, []tstRenderCase{
		{name: "four port shapes and a range", variant: "docker", step: tstRunStep(ports), argv: want},
		{name: "four port shapes and a range", variant: "wslc", step: tstRunStep(ports), argv: want},
	})
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
	tstRender(t, []tstRenderCase{
		{
			name: "bind with ro selinux and propagation", variant: "docker",
			step: tstRunStep(bindFull),
			argv: tstRunArgv([]string{"-v", "/host:/ctr:ro,z,rprivate"}),
		},
		{
			name: "bind plain", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/a", Target: "/b"}}
			}),
			argv: tstRunArgv([]string{"-v", "/a:/b"}),
		},
		{
			name: "bind read-only", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/a", Target: "/b", ReadOnly: true}}
			}),
			argv: tstRunArgv([]string{"-v", "/a:/b:ro"}),
		},
		{
			name: "volume mount", variant: "docker",
			step: tstRunStep(volumeFull),
			argv: tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data,readonly,volume-nocopy,volume-subpath=html"}),
		},
		{
			name: "anonymous volume mount", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "volume", Target: "/data"}}
			}),
			argv: tstRunArgv([]string{"--mount", "type=volume,target=/data"}),
		},
		{
			name: "tmpfs mount", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "tmpfs", Target: "/tmp", Tmpfs: &plan.TmpfsOptions{Size: 64 << 20, Mode: 0o1777}}}
			}),
			argv: tstRunArgv([]string{"--mount", "type=tmpfs,target=/tmp,tmpfs-size=67108864,tmpfs-mode=1777"}),
		},
		{
			name: "image mount", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "image", Source: "nginx:1.27", Target: "/img", Image: &plan.ImageOptions{Subpath: "usr/share"}}}
			}),
			argv: tstRunArgv([]string{"--mount", "type=image,source=nginx:1.27,target=/img,image-subpath=usr/share"}),
		},
		{
			name: "tmpfs paths", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Tmpfs = []string{"/run", "/tmp:size=1m"} }),
			argv: tstRunArgv([]string{"--tmpfs", "/run", "--tmpfs", "/tmp:size=1m"}),
		},
		{
			name: "shm size read-only volumes-from", variant: "docker",
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
			name: "consistency and volume labels are ignored", variant: "docker",
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
			name: "bind recursive disabled is ignored", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Mounts = []plan.Mount{{Type: "bind", Source: "/a", Target: "/b", Bind: &plan.BindOptions{Recursive: "disabled"}}}
			}),
			argv:  tstRunArgv([]string{"-v", "/a:/b"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./b.bind.recursive")},
		},
		{
			name: "volume subpath is ignored", variant: "nerdctl",
			step:  tstRunStep(volumeFull),
			argv:  tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data,readonly,volume-nocopy"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./data.volume.subpath")},
		},
		{
			name: "volume nocopy is ignored", variant: "podman",
			step:  tstRunStep(volumeFull),
			argv:  tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data,readonly,volume-subpath=html"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./data.volume.nocopy")},
		},
		{
			name: "bind keeps only ro", variant: "wslc",
			step:  tstRunStep(bindFull),
			argv:  tstRunArgv([]string{"-v", "/host:/ctr:ro"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./ctr.bind.propagation"), tstIgnored(tstWeb, "volumes./ctr.bind.selinux")},
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
	tstRender(t, []tstRenderCase{
		{
			name: "cmd-shell with interval and retries", variant: "docker",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD-SHELL", "curl -f http://x/ || exit 1"}, Interval: 30 * time.Second, Retries: 3})),
			argv: tstRunArgv([]string{"--health-cmd", "curl -f http://x/ || exit 1", "--health-interval", "30s", "--health-retries", "3"}),
		},
		{
			name: "cmd list is joined", variant: "docker",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD", "curl", "-f", "http://x/"}})),
			argv: tstRunArgv([]string{"--health-cmd", "curl -f http://x/"}),
		},
		{
			name: "cmd list quotes whitespace", variant: "docker",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD", "sh", "-c", "a b"}})),
			argv: tstRunArgv([]string{"--health-cmd", "sh -c 'a b'"}),
		},
		{
			name: "bare list is joined", variant: "docker",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"curl", "-f", "http://x/"}})),
			argv: tstRunArgv([]string{"--health-cmd", "curl -f http://x/"}),
		},
		{
			name: "none disables", variant: "docker",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"NONE"}, Interval: 30 * time.Second})),
			argv: tstRunArgv([]string{"--no-healthcheck"}),
		},
		{
			name: "disable wins over test", variant: "docker",
			step: tstRunStep(health(plan.Healthcheck{Test: []string{"CMD", "true"}, Retries: 3, Disable: true})),
			argv: tstRunArgv([]string{"--no-healthcheck"}),
		},
		{
			name: "all durations", variant: "docker",
			step: tstRunStep(health(full)),
			argv: tstRunArgv([]string{
				"--health-cmd", "curl -f http://x/ || exit 1",
				"--health-interval", "30s", "--health-timeout", "5s",
				"--health-start-period", "10s", "--health-start-interval", "2s",
				"--health-retries", "3",
			}),
		},
		{
			name: "start interval is ignored", variant: "podman",
			step: tstRunStep(health(full)),
			argv: tstRunArgv([]string{
				"--health-cmd", "curl -f http://x/ || exit 1",
				"--health-interval", "30s", "--health-timeout", "5s",
				"--health-start-period", "10s",
				"--health-retries", "3",
			}),
			warns: []tstWarn{tstIgnored(tstWeb, "healthcheck.start_interval")},
		},
	})
}

func TestRunNetworks(t *testing.T) {
	two := func(c *plan.ContainerSpec) {
		c.Networks = []plan.NetworkAttachment{
			{Key: "front", Name: "proj_front", Aliases: []string{"web", "a"}},
			{Key: "back", Name: "proj_back", Aliases: []string{"web"}, IPv4: "10.0.1.5"},
		}
	}
	tstRender(t, []tstRenderCase{
		{
			name: "first network flags and second network keys", variant: "docker",
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
			name: "every address field", variant: "docker",
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
			name: "driver opts gw priority and interface name are ignored", variant: "docker",
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
			name: "aliases are unioned and second network ip is ignored", variant: "podman",
			step:  tstRunStep(two),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "proj_back"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.back.ipv4_address")},
		},
		{
			name: "second network keeps alias only", variant: "wslc",
			step:  tstRunStep(two),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "name=proj_back,alias=web"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.back.ipv4_address")},
		},
		{
			name: "service alias becomes hostname and other aliases are ignored", variant: "nerdctl",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web", "a"}}}
			}),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--hostname", "web"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.front.aliases")},
		},
		{
			name: "service alias alone is silent", variant: "nerdctl",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web"}}}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front", "--hostname", "web"}),
		},
		{
			name: "explicit hostname keeps aliases ignored", variant: "nerdctl",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", Aliases: []string{"web"}}}
				c.Hostname = "custom"
			}),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--hostname", "custom"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.front.aliases")},
		},
		{
			name: "no aliases no hostname", variant: "nerdctl",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front"}}
			}),
			argv: tstRunArgv([]string{"--network", "proj_front"}),
		},
		{
			name: "network mode wins over networks", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.NetworkMode = "host"
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front"}}
			}),
			argv:  tstRunArgv([]string{"--network", "host"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks")},
		},
		{
			name: "network mode alone", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.NetworkMode = "bridge" }),
			argv: tstRunArgv([]string{"--network", "bridge"}),
		},
	})
}

func TestRunResources(t *testing.T) {
	cpus := func(v float64) func(c *plan.ContainerSpec) {
		return func(c *plan.ContainerSpec) { c.CPUs = v }
	}
	tstRender(t, []tstRenderCase{
		{
			name: "fractional cpus round up", variant: "wslc",
			step:  tstRunStep(cpus(1.5)),
			argv:  tstRunArgv([]string{"--cpus", "2"}),
			warns: []tstWarn{tstDegraded(tstWeb, "cpus", "2")},
		},
		{name: "whole cpus", variant: "wslc", step: tstRunStep(cpus(2)), argv: tstRunArgv([]string{"--cpus", "2"})},
		{name: "fractional cpus", variant: "docker", step: tstRunStep(cpus(1.5)), argv: tstRunArgv([]string{"--cpus", "1.5"})},
		{name: "whole cpus", variant: "docker", step: tstRunStep(cpus(2)), argv: tstRunArgv([]string{"--cpus", "2"})},
		{
			name: "cpu flags", variant: "docker",
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
			name: "memory and pids", variant: "docker",
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
			name: "blkio", variant: "docker",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Blkio = &plan.Blkio{
					Weight:          500,
					WeightDevice:    []plan.WeightDevice{{Path: "/dev/sda", Weight: 400}},
					DeviceReadBps:   []plan.ThrottleDevice{{Path: "/dev/sda", Rate: 1 << 20}},
					DeviceWriteBps:  []plan.ThrottleDevice{{Path: "/dev/sdb", Rate: 2 << 20}},
					DeviceReadIOps:  []plan.ThrottleDevice{{Path: "/dev/sda", Rate: 1000}},
					DeviceWriteIOps: []plan.ThrottleDevice{{Path: "/dev/sdb", Rate: 2000}},
				}
			}),
			argv: tstRunArgv([]string{
				"--blkio-weight", "500", "--blkio-weight-device", "/dev/sda:400",
				"--device-read-bps", "/dev/sda:1048576", "--device-write-bps", "/dev/sdb:2097152",
				"--device-read-iops", "/dev/sda:1000", "--device-write-iops", "/dev/sdb:2000",
			}),
		},
	})
}

func TestRunGpus(t *testing.T) {
	step := tstRunStep(func(c *plan.ContainerSpec) {
		c.Gpus = []plan.DeviceRequest{{Driver: "nvidia", Count: 1, IDs: []string{"0"}, Capabilities: []string{"gpu"}}}
	})
	argv, warnings, err := tstNew(t, "docker").Render(step)
	if err != nil {
		t.Fatalf("Render error: %v", err)
	}
	i := slices.Index(argv, "--gpus")
	if i < 0 || i+1 >= len(argv) || argv[i+1] == "" || strings.HasPrefix(argv[i+1], "-") {
		t.Errorf("argv = %q, want --gpus followed by a value", argv)
	}
	tstCheckWarnings(t, warnings, nil)
}

func TestRunVariantGaps(t *testing.T) {
	gap := func(variant, attribute string, f func(c *plan.ContainerSpec)) tstRenderCase {
		return tstRenderCase{name: attribute + " is ignored", variant: variant, step: tstRunStep(f), argv: tstRunArgv(nil), warns: []tstWarn{tstIgnored(tstWeb, attribute)}}
	}
	tstRender(t, []tstRenderCase{
		gap("wslc", "restart", func(c *plan.ContainerSpec) { c.Restart = "always" }),
		gap("wslc", "privileged", func(c *plan.ContainerSpec) { c.Privileged = true }),
		gap("wslc", "cap_add", func(c *plan.ContainerSpec) { c.CapAdd = []string{"NET_ADMIN"} }),
		gap("wslc", "logging", func(c *plan.ContainerSpec) { c.Logging = &plan.Logging{Driver: "json-file"} }),
		gap("wslc", "annotations", func(c *plan.ContainerSpec) {
			c.Annotations = []plan.KV{{Key: "com.example.owner", Value: "platform"}}
		}),
		gap("wslc", "init", func(c *plan.ContainerSpec) { c.Init = tstPtr(true) }),
		gap("wslc", "platform", func(c *plan.ContainerSpec) { c.Platform = "linux/amd64" }),
		gap("wslc", "mac_address", func(c *plan.ContainerSpec) { c.MacAddress = "02:42:ac:1c:00:0a" }),
		gap("nerdctl", "links", func(c *plan.ContainerSpec) { c.Links = []string{"proj-db-1:db"} }),
		gap("nerdctl", "expose", func(c *plan.ContainerSpec) { c.Expose = []string{"8081"} }),
		gap("podman", "links", func(c *plan.ContainerSpec) { c.Links = []string{"proj-db-1:db"} }),
		gap("podman", "isolation", func(c *plan.ContainerSpec) { c.Isolation = "default" }),
		gap("podman", "cpu_count", func(c *plan.ContainerSpec) { c.CPUCount = 2 }),
	})
}

func TestOtherSteps(t *testing.T) {
	network := plan.CreateNetwork{Network: plan.NetworkSpec{
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
	volume := plan.CreateVolume{Volume: plan.VolumeSpec{
		Name:       "proj_data",
		Driver:     "local",
		DriverOpts: []plan.KV{{Key: "device", Value: "tmpfs"}, {Key: "o", Value: "size=100m"}, {Key: "type", Value: "tmpfs"}},
		Labels:     []plan.KV{{Key: plan.LabelProject, Value: "proj"}, {Key: plan.LabelVolume, Value: "data"}},
	}}
	build := plan.BuildImage{Build: plan.BuildSpec{
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
	// buildNoTags keeps wslc's expectations to the flags the contract names for it.
	buildNoTags := build
	buildNoTags.Build.Tags = nil
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
	buildWSLCWarns := []tstWarn{}
	for _, attr := range []string{"platforms", "cache_from", "cache_to", "ssh", "secrets", "network", "shm_size", "extra_hosts", "ulimits", "isolation", "additional_contexts", "privileged"} {
		buildWSLCWarns = append(buildWSLCWarns, tstIgnored(tstWeb, "build."+attr))
	}

	tstRender(t, []tstRenderCase{
		{
			name: "create network all fields", variant: "docker", step: network,
			argv: slices.Concat(networkCommon, []string{"--attachable", "--ipv6", "--ipv4=false"}, networkLabels),
		},
		{
			name: "create network all fields", variant: "nerdctl", step: network,
			argv:  slices.Concat(networkCommon, []string{"--ipv6", "--ipv4=false"}, networkLabels),
			warns: []tstWarn{tstIgnored("networks.proj_front", "attachable")},
		},
		{
			name: "create network all fields", variant: "podman", step: network,
			argv:  slices.Concat(networkCommon, []string{"--ipv6"}, networkLabels),
			warns: []tstWarn{tstIgnored("networks.proj_front", "attachable"), tstIgnored("networks.proj_front", "enable_ipv4")},
		},
		{
			name: "create network all fields", variant: "wslc", step: network,
			argv: slices.Concat([]string{
				"network", "create", "--driver", "bridge", "--opt", "com.docker.network.bridge.name=br-front",
				"--subnet", "172.28.0.0/16", "--gateway", "172.28.0.1", "--ip-range", "172.28.5.0/24", "--internal",
			}, networkLabels),
			warns: []tstWarn{
				tstIgnored("networks.proj_front", "attachable"),
				tstIgnored("networks.proj_front", "enable_ipv4"),
				tstIgnored("networks.proj_front", "enable_ipv6"),
				tstIgnored("networks.proj_front", "ipam.config.aux_addresses"),
				tstIgnored("networks.proj_front", "ipam.driver"),
				tstIgnored("networks.proj_front", "ipam.options"),
			},
		},
		{
			name: "create network minimal", variant: "docker",
			step: plan.CreateNetwork{Network: plan.NetworkSpec{Name: "proj_default", EnableIPv4: tstPtr(true), EnableIPv6: tstPtr(false)}},
			argv: []string{"network", "create", "proj_default"},
		},
		{
			name: "create volume all fields", variant: "docker", step: volume,
			argv: []string{
				"volume", "create", "--driver", "local",
				"--opt", "device=tmpfs", "--opt", "o=size=100m", "--opt", "type=tmpfs",
				"--label", "com.docker.compose.project=proj", "--label", "com.docker.compose.volume=data", "proj_data",
			},
		},
		{
			name: "create volume driver and opts are ignored", variant: "nerdctl", step: volume,
			argv:  []string{"volume", "create", "--label", "com.docker.compose.project=proj", "--label", "com.docker.compose.volume=data", "proj_data"},
			warns: []tstWarn{tstIgnored("volumes.proj_data", "driver"), tstIgnored("volumes.proj_data", "driver_opts")},
		},
		{
			name: "create volume minimal", variant: "podman",
			step: plan.CreateVolume{Volume: plan.VolumeSpec{Name: "proj_data"}},
			argv: []string{"volume", "create", "proj_data"},
		},
		{
			name: "build all fields", variant: "docker", step: build,
			argv: slices.Concat(buildCommon, []string{"--allow", "security.insecure", "/srv/proj/web"}),
		},
		{
			name: "build privileged is ignored", variant: "podman", step: build,
			argv:  slices.Concat(buildCommon, []string{"/srv/proj/web"}),
			warns: []tstWarn{tstIgnored(tstWeb, "build.privileged")},
		},
		{
			name: "build shm size ulimits and isolation are ignored", variant: "nerdctl", step: build,
			argv: []string{
				"build", "-t", "proj-web", "-t", "registry.example.test/proj-web:1.0", "-f", "/srv/proj/web/Dockerfile",
				"--build-arg", "NGINX_VERSION=1.27", "--target", "runtime",
				"--label", "org.opencontainers.image.title=web", "--no-cache", "--pull",
				"--platform", "linux/amd64,linux/arm64",
				"--cache-from", "type=registry,ref=registry.example.test/proj-web:cache", "--cache-to", "type=inline",
				"--ssh", "default", "--secret", "id=npmrc,src=/srv/proj/.npmrc", "--secret", "id=token",
				"--network", "host", "--add-host", "db:10.0.0.5",
				"--build-context", "assets=/srv/proj/assets",
				"--allow", "security.insecure", "/srv/proj/web",
			},
			warns: []tstWarn{tstIgnored(tstWeb, "build.shm_size"), tstIgnored(tstWeb, "build.ulimits"), tstIgnored(tstWeb, "build.isolation")},
		},
		{
			name: "build keeps only the basic flags", variant: "wslc", step: buildNoTags,
			argv: []string{
				"build", "-t", "proj-web", "-f", "/srv/proj/web/Dockerfile",
				"--build-arg", "NGINX_VERSION=1.27", "--target", "runtime",
				"--label", "org.opencontainers.image.title=web", "--no-cache", "--pull", "/srv/proj/web",
			},
			warns: buildWSLCWarns,
		},
		{
			name: "build minimal", variant: "docker",
			step: plan.BuildImage{Build: plan.BuildSpec{Service: "web", Tag: "proj-web", Context: "."}},
			argv: []string{"build", "-t", "proj-web", "."},
		},
		{name: "start many", variant: "docker", step: plan.StartContainers{Names: []string{"a", "b"}}, argv: []string{"start", "a", "b"}},
		{name: "stop without timeout", variant: "docker", step: plan.StopContainers{Names: []string{"a"}}, argv: []string{"stop", "a"}},
		{
			name: "stop timeout rounds up", variant: "docker",
			step: plan.StopContainers{Names: []string{"a", "b"}, Timeout: tstPtr(1500 * time.Millisecond)},
			argv: []string{"stop", "--time", "2", "a", "b"},
		},
		{
			name: "stop whole timeout", variant: "podman",
			step: plan.StopContainers{Names: []string{"a"}, Timeout: tstPtr(10 * time.Second)},
			argv: []string{"stop", "--time", "10", "a"},
		},
		{name: "remove containers", variant: "docker", step: plan.RemoveContainers{Names: []string{"a", "b"}}, argv: []string{"rm", "-f", "a", "b"}},
		{name: "remove containers with volumes", variant: "docker", step: plan.RemoveContainers{Names: []string{"a"}, Volumes: true}, argv: []string{"rm", "-f", "-v", "a"}},
		{name: "remove network", variant: "docker", step: plan.RemoveNetwork{Name: "proj_front"}, argv: []string{"network", "rm", "proj_front"}},
		{name: "remove volume", variant: "docker", step: plan.RemoveVolume{Name: "proj_data"}, argv: []string{"volume", "rm", "proj_data"}},
		{name: "list containers without project", variant: "docker", step: plan.ListContainers{}, argv: []string{"ps", "-a", "--no-trunc", "--format", "{{json .}}"}},
		{
			name: "list containers with project", variant: "docker", step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--no-trunc", "--filter", "label=com.docker.compose.project=proj", "--format", "{{json .}}"},
		},
		{
			name: "list containers with project", variant: "nerdctl", step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--no-trunc", "--filter", "label=com.docker.compose.project=proj", "--format", "{{json .}}"},
		},
		{name: "list containers without project", variant: "podman", step: plan.ListContainers{}, argv: []string{"ps", "-a", "--format", "json"}},
		{
			name: "list containers with project", variant: "podman", step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--filter", "label=com.docker.compose.project=proj", "--format", "json"},
		},
		{
			name: "list containers with project", variant: "wslc", step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--filter", "label=com.docker.compose.project=proj", "--format", "json"},
		},
		{name: "list networks", variant: "docker", step: plan.ListNetworks{}, argv: []string{"network", "ls", "--format", "{{json .}}"}},
		{name: "list networks", variant: "podman", step: plan.ListNetworks{}, argv: []string{"network", "ls", "--format", "json"}},
		{name: "list volumes", variant: "nerdctl", step: plan.ListVolumes{}, argv: []string{"volume", "ls", "--format", "{{json .}}"}},
		{name: "list volumes", variant: "wslc", step: plan.ListVolumes{}, argv: []string{"volume", "ls", "--format", "json"}},
		{name: "list images", variant: "docker", step: plan.ListImages{}, argv: []string{"image", "ls", "--format", "{{json .}}"}},
		{name: "list images", variant: "podman", step: plan.ListImages{}, argv: []string{"image", "ls", "--format", "json"}},
		{name: "inspect", variant: "docker", step: plan.InspectContainer{Name: "proj-web-1"}, argv: []string{"inspect", "proj-web-1"}},
		{name: "logs plain", variant: "docker", step: plan.Logs{Name: "proj-web-1"}, argv: []string{"logs", "proj-web-1"}},
		{name: "logs tail all is omitted", variant: "docker", step: plan.Logs{Name: "proj-web-1", Tail: "all"}, argv: []string{"logs", "proj-web-1"}},
		{name: "logs follow and tail", variant: "docker", step: plan.Logs{Name: "proj-web-1", Follow: true, Tail: "10"}, argv: []string{"logs", "-f", "--tail", "10", "proj-web-1"}},
	})

	for _, v := range tstVariants {
		t.Run(v+"/wait completed renders nothing", func(t *testing.T) {
			argv, warnings, err := tstNew(t, v).Render(plan.WaitContainer{Name: "proj-web-1", Condition: plan.ConditionCompleted})
			if err != nil || len(argv) != 0 || len(warnings) != 0 {
				t.Errorf("Render(WaitContainer) = %q, %q, %v; want empty argv, no warnings, nil error", argv, warnings, err)
			}
		})
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
	for _, v := range tstVariants {
		e := tstNew(t, v)
		for _, tt := range cases {
			t.Run(v+"/"+tt.name, func(t *testing.T) {
				argv, _, err := e.Render(tt.step)
				if err == nil {
					t.Errorf("Render(%T) = %q, nil error; want an error", tt.step, argv)
				}
				if argv != nil {
					t.Errorf("Render(%T) argv = %q, want nil", tt.step, argv)
				}
			})
		}
	}
}

func TestDeterministic(t *testing.T) {
	for _, v := range tstVariants {
		t.Run(v, func(t *testing.T) {
			e := tstNew(t, v)
			for _, step := range enginetest.Steps() {
				argv, warnings, err := e.Render(step)
				if err != nil {
					t.Fatalf("Render(%T) error: %v", step, err)
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

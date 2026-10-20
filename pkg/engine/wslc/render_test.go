package wslc_test

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/engine/docker"
	"github.com/wzshiming/my-compose/pkg/engine/wslc"
	"github.com/wzshiming/my-compose/pkg/plan"
)

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
	name  string
	step  plan.Step
	argv  []string
	warns []tstWarn
}

// tstRender renders every case and checks its exact argv and its warnings.
func tstRender(t *testing.T, cases []tstRenderCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			argv, warnings, err := wslc.New().Render(tt.step)
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

func TestRunPortsMatchDocker(t *testing.T) {
	step := tstRunStep(func(c *plan.ContainerSpec) {
		c.Ports = []plan.Port{
			{Target: 80},
			{Published: "8080", Target: 80, Protocol: "tcp"},
			{HostIP: "127.0.0.1", Published: "8080", Target: 80},
			{HostIP: "127.0.0.1", Target: 80, Protocol: "udp"},
			{Published: "8000-8010", Target: 8000},
		}
	})
	want := tstRunArgv([]string{
		"-p", "80",
		"-p", "8080:80/tcp",
		"-p", "127.0.0.1:8080:80",
		"-p", "127.0.0.1::80/udp",
		"-p", "8000-8010:8000",
	})
	var argvs [][]string
	for _, e := range []engine.Engine{wslc.New(), docker.New()} {
		argv, warnings, err := e.Render(step)
		if err != nil {
			t.Fatalf("%s: Render error: %v", e.Name(), err)
		}
		if !reflect.DeepEqual(argv, want) {
			t.Errorf("%s: argv =\n  %q\nwant\n  %q", e.Name(), argv, want)
		}
		tstCheckWarnings(t, warnings, nil)
		argvs = append(argvs, argv)
	}
	if !reflect.DeepEqual(argvs[0], argvs[1]) {
		t.Errorf("wslc argv =\n  %q\ndocker argv =\n  %q", argvs[0], argvs[1])
	}
}

func TestRunMounts(t *testing.T) {
	bindFull := func(c *plan.ContainerSpec) {
		c.Mounts = []plan.Mount{{
			Type: "bind", Source: "/host", Target: "/ctr", ReadOnly: true,
			Bind: &plan.BindOptions{Propagation: "rprivate", SELinux: "z", CreateHostPath: true, Recursive: "enabled"},
		}}
	}
	tstRender(t, []tstRenderCase{
		{
			name:  "bind keeps only ro",
			step:  tstRunStep(bindFull),
			argv:  tstRunArgv([]string{"-v", "/host:/ctr:ro"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./ctr.bind.propagation"), tstIgnored(tstWeb, "volumes./ctr.bind.selinux")},
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
			name:  "second network keeps alias only",
			step:  tstRunStep(two),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "name=proj_back,alias=web"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.back.ipv4_address")},
		},
		{
			name: "ipv6 address is ignored",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", IPv6: "fd00::5"}}
			}),
			argv:  tstRunArgv([]string{"--network", "proj_front"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.front.ipv6_address")},
		},
		{
			name: "link local ips are ignored",
			step: tstRunStep(func(c *plan.ContainerSpec) {
				c.Networks = []plan.NetworkAttachment{{Key: "front", Name: "proj_front", LinkLocalIPs: []string{"169.254.0.5"}}}
			}),
			argv:  tstRunArgv([]string{"--network", "proj_front"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.front.link_local_ips")},
		},
	})
}

func TestRunResources(t *testing.T) {
	cpus := func(v float64) func(c *plan.ContainerSpec) {
		return func(c *plan.ContainerSpec) { c.CPUs = v }
	}
	tstRender(t, []tstRenderCase{
		{
			name:  "fractional cpus round up",
			step:  tstRunStep(cpus(1.5)),
			argv:  tstRunArgv([]string{"--cpus", "2"}),
			warns: []tstWarn{tstDegraded(tstWeb, "cpus", "2")},
		},
		{name: "whole cpus", step: tstRunStep(cpus(2)), argv: tstRunArgv([]string{"--cpus", "2"})},
	})
}

func TestRunVariantGaps(t *testing.T) {
	gap := func(attribute string, f func(c *plan.ContainerSpec)) tstRenderCase {
		return tstRenderCase{name: attribute + " is ignored", step: tstRunStep(f), argv: tstRunArgv(nil), warns: []tstWarn{tstIgnored(tstWeb, attribute)}}
	}
	tstRender(t, []tstRenderCase{
		gap("restart", func(c *plan.ContainerSpec) { c.Restart = "always" }),
		gap("privileged", func(c *plan.ContainerSpec) { c.Privileged = true }),
		gap("cap_add", func(c *plan.ContainerSpec) { c.CapAdd = []string{"NET_ADMIN"} }),
		gap("logging", func(c *plan.ContainerSpec) { c.Logging = &plan.Logging{Driver: "json-file"} }),
		gap("annotations", func(c *plan.ContainerSpec) {
			c.Annotations = []plan.KV{{Key: "com.example.owner", Value: "platform"}}
		}),
		gap("init", func(c *plan.ContainerSpec) { c.Init = tstPtr(true) }),
		gap("platform", func(c *plan.ContainerSpec) { c.Platform = "linux/amd64" }),
		gap("mac_address", func(c *plan.ContainerSpec) { c.MacAddress = "02:42:ac:1c:00:0a" }),
		gap("healthcheck.start_interval", func(c *plan.ContainerSpec) {
			c.Healthcheck = &plan.Healthcheck{StartInterval: 2 * time.Second}
		}),
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
	// buildNoTags keeps the expectations to the flags the contract names for wslc.
	buildNoTags := build
	buildNoTags.Build.Tags = nil
	networkLabels := []string{"--label", "com.docker.compose.network=front", "--label", "com.docker.compose.project=proj", "proj_front"}
	buildWarns := []tstWarn{}
	for _, attr := range []string{"platforms", "cache_from", "cache_to", "ssh", "secrets", "network", "shm_size", "extra_hosts", "ulimits", "isolation", "additional_contexts", "privileged"} {
		buildWarns = append(buildWarns, tstIgnored(tstWeb, "build."+attr))
	}

	tstRender(t, []tstRenderCase{
		{
			name: "create network all fields", step: network,
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
			name: "build keeps only the basic flags", step: buildNoTags,
			argv: []string{
				"build", "-t", "proj-web", "-f", "/srv/proj/web/Dockerfile",
				"--build-arg", "NGINX_VERSION=1.27", "--target", "runtime",
				"--label", "org.opencontainers.image.title=web", "--no-cache", "--pull", "/srv/proj/web",
			},
			warns: buildWarns,
		},
		{
			name: "list containers with project", step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--filter", "label=com.docker.compose.project=proj", "--format", "json"},
		},
		{name: "list volumes", step: plan.ListVolumes{}, argv: []string{"volume", "ls", "--format", "json"}},
	})
}

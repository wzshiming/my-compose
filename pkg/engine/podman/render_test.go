package podman_test

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine/podman"
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
			argv, warnings, err := podman.New().Render(tt.step)
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

func TestRunMounts(t *testing.T) {
	volumeFull := func(c *plan.ContainerSpec) {
		c.Mounts = []plan.Mount{{
			Type: "volume", Source: "data", Target: "/data", ReadOnly: true,
			Volume: &plan.VolumeOptions{NoCopy: true, Subpath: "html"},
		}}
	}
	tstRender(t, []tstRenderCase{
		{
			name:  "volume nocopy is ignored",
			step:  tstRunStep(volumeFull),
			argv:  tstRunArgv([]string{"--mount", "type=volume,source=data,target=/data,readonly,volume-subpath=html"}),
			warns: []tstWarn{tstIgnored(tstWeb, "volumes./data.volume.nocopy")},
		},
	})
}

func TestRunHealthcheck(t *testing.T) {
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
			name: "start interval is ignored",
			step: tstRunStep(func(c *plan.ContainerSpec) { c.Healthcheck = &full }),
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
			name:  "aliases are unioned and second network ip is ignored",
			step:  tstRunStep(two),
			argv:  tstRunArgv([]string{"--network", "proj_front", "--network-alias", "web", "--network-alias", "a", "--network", "proj_back"}),
			warns: []tstWarn{tstIgnored(tstWeb, "networks.back.ipv4_address")},
		},
	})
}

func TestRunGaps(t *testing.T) {
	gap := func(attribute string, f func(c *plan.ContainerSpec)) tstRenderCase {
		return tstRenderCase{name: attribute + " is ignored", step: tstRunStep(f), argv: tstRunArgv(nil), warns: []tstWarn{tstIgnored(tstWeb, attribute)}}
	}
	tstRender(t, []tstRenderCase{
		gap("links", func(c *plan.ContainerSpec) { c.Links = []string{"proj-db-1:db"} }),
		gap("isolation", func(c *plan.ContainerSpec) { c.Isolation = "default" }),
		gap("cpu_count", func(c *plan.ContainerSpec) { c.CPUCount = 2 }),
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

	tstRender(t, []tstRenderCase{
		{
			name: "create network all fields", step: network,
			argv:  slices.Concat(networkCommon, []string{"--ipv6"}, networkLabels),
			warns: []tstWarn{tstIgnored("networks.proj_front", "attachable"), tstIgnored("networks.proj_front", "enable_ipv4")},
		},
		{
			name: "build privileged is ignored", step: build,
			argv:  slices.Concat(buildCommon, []string{"/srv/proj/web"}),
			warns: []tstWarn{tstIgnored(tstWeb, "build.privileged")},
		},
		{name: "list containers without project", step: plan.ListContainers{}, argv: []string{"ps", "-a", "--format", "json"}},
		{
			name: "list containers with project", step: plan.ListContainers{Project: "proj"},
			argv: []string{"ps", "-a", "--filter", "label=com.docker.compose.project=proj", "--format", "json"},
		},
		{name: "list networks", step: plan.ListNetworks{}, argv: []string{"network", "ls", "--format", "json"}},
		{name: "list volumes", step: plan.ListVolumes{}, argv: []string{"volume", "ls", "--format", "json"}},
		{name: "list images", step: plan.ListImages{}, argv: []string{"image", "ls", "--format", "json"}},
	})
}

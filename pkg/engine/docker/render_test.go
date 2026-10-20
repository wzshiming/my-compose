package docker_test

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/docker"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// tstWarn is an expected warning: exact Scope and Attribute, but only a Message prefix since the parenthesised reason is free text.
type tstWarn struct {
	Scope, Attribute, Prefix string
}

func (w tstWarn) String() string { return w.Scope + ": " + w.Attribute + ": " + w.Prefix + "..." }

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
			argv, warnings, err := docker.New().Render(tt.step)
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

func TestRunNetworks(t *testing.T) {
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
	})
}

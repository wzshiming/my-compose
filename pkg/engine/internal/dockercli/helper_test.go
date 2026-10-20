package dockercli_test

import (
	"cmp"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/internal/dockercli"
	"github.com/wzshiming/my-compose/pkg/plan"
)

const tstWeb = "services.web"

// tstVariant is a table-driven Variant like dockerlike's rows; bare makes Has answer false for every flag.
type tstVariant struct {
	name, binary, listFormat string
	psNoTrunc, integerCPUs   bool
	bare, healthcheck        bool
	aliases                  dockercli.AliasMode
	networkKeys, missing     []string
}

func (v tstVariant) Name() string   { return v.name }
func (v tstVariant) Binary() string { return v.binary }
func (v tstVariant) Capabilities() plan.Capabilities {
	return plan.Capabilities{Healthcheck: v.healthcheck}
}
func (v tstVariant) ListFormat() string           { return v.listFormat }
func (v tstVariant) PSNoTrunc() bool              { return v.psNoTrunc }
func (v tstVariant) IntegerCPUs() bool            { return v.integerCPUs }
func (v tstVariant) Aliases() dockercli.AliasMode { return v.aliases }
func (v tstVariant) NetworkKeys() []string        { return v.networkKeys }

func (v tstVariant) Has(command, flag string) bool {
	return !v.bare && !slices.Contains(v.missing, command+" "+flag)
}

// tstFull is the docker-shaped table: every flag present, --no-trunc, {{json .}}, aliases on the first network, every network key.
func tstFull() tstVariant {
	return tstVariant{
		name: "stub", binary: "stub-cli", listFormat: "{{json .}}",
		psNoTrunc: true, healthcheck: true,
		aliases:     dockercli.AliasFirst,
		networkKeys: []string{"alias", "ip", "ip6", "mac-address", "link-local-ip"},
	}
}

// tstBare lacks every flag.
func tstBare() tstVariant {
	v := tstFull()
	v.bare = true
	return v
}

// tstAliasAll binds every alias to the first network and takes no --network keys.
func tstAliasAll() tstVariant {
	v := tstFull()
	v.aliases = dockercli.AliasAll
	v.networkKeys = nil
	return v
}

// tstAliasHostname degrades the service alias to --hostname and has no --network-alias.
func tstAliasHostname() tstVariant {
	v := tstFull()
	v.aliases = dockercli.AliasHostname
	v.missing = []string{"run --network-alias"}
	return v
}

// tstIntCPUs takes whole cpus only.
func tstIntCPUs() tstVariant {
	v := tstFull()
	v.integerCPUs = true
	return v
}

// tstNoHealth reports no healthcheck support.
func tstNoHealth() tstVariant {
	v := tstFull()
	v.healthcheck = false
	return v
}

// tstMissing is the full table minus the given "<command> <flag>" keys.
func tstMissing(keys ...string) tstVariant {
	v := tstFull()
	v.missing = keys
	return v
}

// tstConfig is a labelled stub configuration.
type tstConfig struct {
	label string
	v     dockercli.Variant
}

// tstConfigs lists every stub configuration.
func tstConfigs() []tstConfig {
	return []tstConfig{
		{"full", tstFull()},
		{"bare", tstBare()},
		{"aliasAll", tstAliasAll()},
		{"aliasHostname", tstAliasHostname()},
		{"intCPUs", tstIntCPUs()},
		{"noHealth", tstNoHealth()},
		{"missing", tstMissing("run --restart", "run --mount volume-nocopy", "build --allow", "network create --ipv4", "volume create --opt")},
		{"recorder", tstNewRecorder()},
	}
}

// tstRecorder is the full table that also records every flag Has is asked, by command.
type tstRecorder struct {
	tstVariant
	asked map[string]map[string]bool
}

func tstNewRecorder() tstRecorder {
	return tstRecorder{tstVariant: tstFull(), asked: map[string]map[string]bool{}}
}

func (r tstRecorder) Has(command, flag string) bool {
	if r.asked[command] == nil {
		r.asked[command] = map[string]bool{}
	}
	r.asked[command][flag] = true
	return r.tstVariant.Has(command, flag)
}

// commands returns the commands Has was asked about, sorted.
func (r tstRecorder) commands() []string {
	return slices.Sorted(maps.Keys(r.asked))
}

// flags returns the flags Has was asked about under command, sorted.
func (r tstRecorder) flags(command string) []string {
	return slices.Sorted(maps.Keys(r.asked[command]))
}

// tstWarn is an expected warning: exact Scope and Attribute, and a Message prefix; the exact helpers give the whole message.
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

// tstIgnoredFor expects the whole message "ignored (<reason>)".
func tstIgnoredFor(scope, attribute, reason string) tstWarn {
	return tstWarn{Scope: scope, Attribute: attribute, Prefix: "ignored (" + reason + ")"}
}

// tstDegradedFor expects the whole message "degraded to <value> (<reason>)".
func tstDegradedFor(scope, attribute, value, reason string) tstWarn {
	return tstWarn{Scope: scope, Attribute: attribute, Prefix: "degraded to " + value + " (" + reason + ")"}
}

// tstNoFlag is the Has warning for attribute when the stub lacks flag.
func tstNoFlag(scope, attribute, flag string) tstWarn {
	return tstIgnoredFor(scope, attribute, "stub has no "+flag)
}

type tstRenderCase struct {
	name  string
	v     dockercli.Variant // nil renders with tstFull
	step  plan.Step
	argv  []string
	warns []tstWarn
}

// tstRender renders every case and checks its exact argv and its warnings.
func tstRender(t *testing.T, cases []tstRenderCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			v := tt.v
			if v == nil {
				v = tstFull()
			}
			argv, warnings, err := dockercli.New(v).Render(tt.step)
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

func tstEqual[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

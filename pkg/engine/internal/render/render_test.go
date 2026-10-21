package render_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine/internal/render"
	"github.com/wzshiming/my-compose/pkg/plan"
)

func tstEqual[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

// tstIgn and tstDeg are warnings with the fixed reason "r" (and value "v") for order and dedupe cases.
func tstIgn(scope, attr string) plan.Warning {
	return plan.Warning{Scope: scope, Attribute: attr, Message: "ignored (r)"}
}

func tstDeg(scope, attr string) plan.Warning {
	return plan.Warning{Scope: scope, Attribute: attr, Message: "degraded to v (r)"}
}

func TestFinishFresh(t *testing.T) {
	var b render.Builder
	argv, ws := b.Finish()
	tstEqual(t, "argv", argv, []string(nil))
	tstEqual(t, "warnings", ws, []plan.Warning{})
}

func TestArgv(t *testing.T) {
	cases := []struct {
		name  string
		build func(b *render.Builder)
		want  []string
	}{
		{"args", func(b *render.Builder) {
			b.Args("run", "-d")
			b.Args()
			b.Args("img")
		}, []string{"run", "-d", "img"}},
		{"repeat", func(b *render.Builder) {
			b.Repeat("-v", []string{"a:/a", "b:/b"})
		}, []string{"-v", "a:/a", "-v", "b:/b"}},
		{"repeat nil adds nothing", func(b *render.Builder) {
			b.Args("run")
			b.Repeat("-v", nil)
			b.Args("img")
		}, []string{"run", "img"}},
		{"repeat empty adds nothing", func(b *render.Builder) {
			b.Args("run")
			b.Repeat("-v", []string{})
			b.Args("img")
		}, []string{"run", "img"}},
		{"kv in given order", func(b *render.Builder) {
			b.KV("--label", []plan.KV{{Key: "b", Value: "2"}, {Key: "a", Value: "1"}})
		}, []string{"--label", "b=2", "--label", "a=1"}},
		{"kv without dedupe", func(b *render.Builder) {
			b.KV("--label", []plan.KV{{Key: "a", Value: "1"}, {Key: "a", Value: "1"}})
		}, []string{"--label", "a=1", "--label", "a=1"}},
		{"kv empty value", func(b *render.Builder) {
			b.KV("--env", []plan.KV{{Key: "k"}})
		}, []string{"--env", "k="}},
		{"kv nil adds nothing", func(b *render.Builder) {
			b.Args("run")
			b.KV("--label", nil)
			b.Args("img")
		}, []string{"run", "img"}},
		{"finish keeps order", func(b *render.Builder) {
			b.Args("z", "a")
			b.Repeat("-m", []string{"c", "b"})
			b.KV("--l", []plan.KV{{Key: "y", Value: "1"}, {Key: "x", Value: "2"}})
			b.SetScope("s")
			b.Ignored("x", "r")
		}, []string{"z", "a", "-m", "c", "-m", "b", "--l", "y=1", "--l", "x=2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b render.Builder
			c.build(&b)
			argv, _ := b.Finish()
			tstEqual(t, "argv", argv, c.want)
		})
	}
}

func TestWarnings(t *testing.T) {
	cases := []struct {
		name  string
		build func(b *render.Builder)
		want  []plan.Warning
	}{
		{"ignored", func(b *render.Builder) {
			b.SetScope("services.web")
			b.Ignored("cpu_shares", "no such flag")
		}, []plan.Warning{{Scope: "services.web", Attribute: "cpu_shares", Message: "ignored (no such flag)"}}},
		{"degraded", func(b *render.Builder) {
			b.SetScope("services.web")
			b.Degraded("restart", "always", "no on-failure")
		}, []plan.Warning{{Scope: "services.web", Attribute: "restart", Message: "degraded to always (no on-failure)"}}},
		{"ignored if false", func(b *render.Builder) {
			b.SetScope("services.web")
			b.IgnoredIf(false, "cpu_shares", "no such flag")
		}, []plan.Warning{}},
		{"ignored if true", func(b *render.Builder) {
			b.SetScope("services.web")
			b.IgnoredIf(true, "cpu_shares", "no such flag")
		}, []plan.Warning{{Scope: "services.web", Attribute: "cpu_shares", Message: "ignored (no such flag)"}}},
		{"no scope", func(b *render.Builder) {
			b.Ignored("x", "r")
		}, []plan.Warning{{Scope: "", Attribute: "x", Message: "ignored (r)"}}},
		{"scope applies to later warnings only", func(b *render.Builder) {
			b.Ignored("a", "r")
			b.SetScope("networks.n")
			b.Ignored("b", "r")
			b.SetScope("volumes.v")
			b.Ignored("c", "r")
		}, []plan.Warning{tstIgn("", "a"), tstIgn("networks.n", "b"), tstIgn("volumes.v", "c")}},
		{"sorted by scope, attribute, message", func(b *render.Builder) {
			b.SetScope("b")
			b.Degraded("y", "v", "r")
			b.Ignored("x", "r")
			b.SetScope("a")
			b.Ignored("z", "r")
			b.Degraded("z", "v", "r")
		}, []plan.Warning{tstDeg("a", "z"), tstIgn("a", "z"), tstIgn("b", "x"), tstDeg("b", "y")}},
		{"exact duplicates collapsed", func(b *render.Builder) {
			b.SetScope("s")
			b.Ignored("x", "r")
			b.Ignored("y", "r")
			b.Ignored("x", "r")
			b.Ignored("x", "r2")
		}, []plan.Warning{tstIgn("s", "x"), {Scope: "s", Attribute: "x", Message: "ignored (r2)"}, tstIgn("s", "y")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b render.Builder
			c.build(&b)
			_, ws := b.Finish()
			tstEqual(t, "warnings", ws, c.want)
		})
	}
}

func TestEmpty(t *testing.T) {
	cases := []struct {
		step  plan.Step
		field string
		want  string
	}{
		{plan.BuildImage{}, "Tag", "plan.BuildImage: empty Tag"},
		{plan.RunContainer{}, "Name", "plan.RunContainer: empty Name"},
	}
	for _, c := range cases {
		err := render.Empty(c.step, c.field)
		if err == nil {
			t.Errorf("Empty(%T, %q) = nil, want error", c.step, c.field)
			continue
		}
		if got := err.Error(); got != c.want {
			t.Errorf("Empty(%T, %q) = %q, want %q", c.step, c.field, got, c.want)
		}
	}
}

func TestUnsupported(t *testing.T) {
	cases := []struct {
		step plan.Step
		want string
	}{
		{nil, "unsupported step <nil>"},
		{plan.Logs{}, "unsupported step plan.Logs"},
	}
	for _, c := range cases {
		err := render.Unsupported(c.step)
		if err == nil {
			t.Errorf("Unsupported(%T) = nil, want error", c.step)
			continue
		}
		if got := err.Error(); got != c.want {
			t.Errorf("Unsupported(%T) = %q, want %q", c.step, got, c.want)
		}
	}
}

func TestSeconds(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0"},
		{time.Nanosecond, "1"},
		{999 * time.Millisecond, "1"},
		{1500 * time.Millisecond, "2"},
		{10 * time.Second, "10"},
		{-1500 * time.Millisecond, "-1"},
	}
	for _, c := range cases {
		if got := render.Seconds(c.d); got != c.want {
			t.Errorf("Seconds(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

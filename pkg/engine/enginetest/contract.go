package enginetest

import (
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// unknownStep embeds a nil plan.Step to satisfy the sealed interface with a type no engine knows.
type unknownStep struct{ plan.Step }

// Contract reports through tb.Errorf every way e breaks the engine.Engine contract, prefixing each message with the property it checks.
func Contract(tb testing.TB, e engine.Engine) {
	tb.Helper()
	if name := e.Name(); name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		tb.Errorf("Name: %q must be non-empty without whitespace", name)
	}
	if bin := e.Binary(); bin == "" || strings.ContainsFunc(bin, unicode.IsSpace) || strings.ContainsAny(bin, `/\`) {
		tb.Errorf("Binary: %q must be a non-empty command name without whitespace or path separators", bin)
	}
	if a, b := e.Capabilities(), e.Capabilities(); !reflect.DeepEqual(a, b) {
		tb.Errorf("Capabilities: %+v, then %+v", a, b)
	}

	steps, want := Steps(), Steps()
	for i, step := range steps {
		kind := reflect.TypeOf(step).Name()
		argv, warnings, err := e.Render(step)
		_, wait := step.(plan.WaitContainer)
		switch {
		case err != nil:
			tb.Errorf("Render/%s: %v", kind, err)
		case wait && len(argv) != 0:
			tb.Errorf("Render/%s: argv %q, want empty", kind, argv)
		case !wait && (len(argv) == 0 || argv[0] == e.Binary()):
			tb.Errorf("Render/%s: argv %q must be non-empty and not start with Binary %q", kind, argv, e.Binary())
		}
		if argv2, warnings2, _ := e.Render(step); !reflect.DeepEqual(argv, argv2) || !reflect.DeepEqual(warnings, warnings2) {
			tb.Errorf("Render/determinism: %s rendered %q %q, then %q %q", kind, argv, warnings, argv2, warnings2)
		}
		if !reflect.DeepEqual(step, want[i]) {
			tb.Errorf("Render/mutation: %s step modified", kind)
		}
		checkWarnings(tb, kind, warnings)
	}
	if _, _, err := e.Render(nil); err == nil {
		tb.Errorf("Render/nil-step: Render(nil) gave nil error")
	}
	if _, _, err := e.Render(unknownStep{}); err == nil {
		tb.Errorf("Render/unknown-step: Render(%T{}) gave nil error", unknownStep{})
	}
	_, _, err := e.Render(plan.WaitContainer{Name: "x", Condition: plan.ConditionHealthy})
	if healthcheck := e.Capabilities().Healthcheck; (err == nil) != healthcheck {
		tb.Errorf("WaitHealthy: error %v with Capabilities().Healthcheck %t, want an error exactly without healthcheck", err, healthcheck)
	}

	checkList(tb, "ParseContainers", e.ParseContainers)
	checkList(tb, "ParseNetworks", e.ParseNetworks)
	checkList(tb, "ParseVolumes", e.ParseVolumes)
	checkList(tb, "ParseImages", e.ParseImages)
	for _, in := range [][]byte{nil, []byte(""), []byte(" \n\t")} {
		if _, err := e.ParseInspect(in); err == nil {
			tb.Errorf("ParseInspect/empty: %q gave nil error", in)
		}
	}
	if _, err := e.ParseInspect([]byte("not json")); err == nil {
		tb.Errorf("ParseInspect/invalid: %q gave nil error", "not json")
	}
}

func checkWarnings(tb testing.TB, kind string, ws []plan.Warning) {
	tb.Helper()
	for _, w := range ws {
		if !strings.HasPrefix(w.Scope, "services.") && !strings.HasPrefix(w.Scope, "containers.") && !strings.HasPrefix(w.Scope, "networks.") && !strings.HasPrefix(w.Scope, "volumes.") {
			tb.Errorf("Warnings: %s: %q: scope must start with services., containers., networks. or volumes.", kind, w)
		}
		if w.Attribute == "" {
			tb.Errorf("Warnings: %s: %q: empty attribute", kind, w)
		}
		if m := w.Message; (!strings.HasPrefix(m, "ignored (") && !strings.HasPrefix(m, "degraded to ")) || m != strings.ToLower(m) || strings.HasSuffix(m, ".") {
			tb.Errorf(`Warnings: %s: %q: message must be lowercase "ignored (...)" or "degraded to ..." without a trailing period`, kind, w)
		}
	}
	if len(ws) > 0 && !reflect.DeepEqual(ws, plan.NormalizeWarnings(ws)) {
		tb.Errorf("Warnings: %s: %q must be sorted and deduplicated", kind, ws)
	}
}

func checkList[T any](tb testing.TB, method string, parse func([]byte) ([]T, error)) {
	tb.Helper()
	for _, in := range [][]byte{nil, []byte(""), []byte(" \n\t")} {
		if got, err := parse(in); len(got) != 0 || err != nil {
			tb.Errorf("%s/empty: %q gave %v, %v, want an empty slice and nil error", method, in, got, err)
		}
	}
	if _, err := parse([]byte("not json")); err == nil {
		tb.Errorf("%s/invalid: %q gave nil error", method, "not json")
	}
}

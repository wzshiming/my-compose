package plan

import (
	"reflect"
	"slices"
	"testing"
)

func TestWarningString(t *testing.T) {
	w := Warning{"services.web", "ports[0].mode", "ignored (unsupported)"}
	if got, want := w.String(), "services.web: ports[0].mode: ignored (unsupported)"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestNormalizeWarnings(t *testing.T) {
	a := Warning{"networks.front", "labels", "ignored (unsupported)"}
	b := Warning{"services.web", "cpu_count", "ignored (unsupported)"}
	c := Warning{"services.web", "cpu_count", "ignored (windows only)"}
	d := Warning{"services.web", "restart", "degraded to no (unsupported)"}
	tests := []struct {
		name string
		in   []Warning
		want []Warning
	}{
		{"nil", nil, []Warning{}},
		{"sorts by scope, attribute, message", []Warning{d, c, a, b}, []Warning{a, b, c, d}},
		{"drops duplicates", []Warning{b, a, b, a}, []Warning{a, b}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := slices.Clone(tt.in)
			if got := NormalizeWarnings(in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NormalizeWarnings() = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(in, tt.in) {
				t.Errorf("NormalizeWarnings modified its input: %v, want %v", in, tt.in)
			}
		})
	}
}

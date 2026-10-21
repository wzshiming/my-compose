package parse_test

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/internal/parse"
)

type tstItem struct {
	A int `json:"a"`
}

func tstEqual[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", what, got, want)
	}
}

// tstName picks "n<A>" and treats A == 0 as unnamed.
func tstName(it tstItem) string {
	if it.A == 0 {
		return ""
	}
	return "n" + strconv.Itoa(it.A)
}

func TestList(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []tstItem
	}{
		{"nil", nil, []tstItem{}},
		{"empty", []byte(""), []tstItem{}},
		{"whitespace", []byte(" \n\t"), []tstItem{}},
		{"empty array", []byte("[]"), []tstItem{}},
		{"array", []byte("[{},{}]"), []tstItem{{}, {}}},
		{"object stream", []byte("{}\n{}"), []tstItem{{}, {}}},
		{"array stream", []byte("[{}][{}]"), []tstItem{{}, {}}},
		{"object", []byte("{}"), []tstItem{{}}},
		{"field", []byte(`{"a":1}`), []tstItem{{A: 1}}},
		{"order kept", []byte(`[{"a":1},{"a":2}]{"a":3}[{"a":4}]`), []tstItem{{A: 1}, {A: 2}, {A: 3}, {A: 4}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse.List[tstItem](c.in)
			if err != nil {
				t.Fatalf("List(%q) error: %v", c.in, err)
			}
			tstEqual(t, fmt.Sprintf("List(%q)", c.in), got, c.want)
		})
	}
}

func TestListRejects(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		prefix string // "" when any error will do
	}{
		{"number", "42", "want a JSON object or array"},
		{"string", `"s"`, "want a JSON object or array"},
		{"null", "null", "want a JSON object or array"},
		{"truncated array", "[1,2", ""},
		{"not json", "not json", ""},
		{"bom", "\ufeff{}", ""},
		{"object then garbage", "{}\nnot json", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parse.List[tstItem]([]byte(c.in))
			if err == nil {
				t.Fatalf("List(%q) = %#v, want error", c.in, got)
			}
			if got != nil {
				t.Errorf("List(%q) = %#v with error, want nil", c.in, got)
			}
			if !strings.HasPrefix(err.Error(), c.prefix) {
				t.Errorf("List(%q) error = %q, want prefix %q", c.in, err, c.prefix)
			}
		})
	}
}

func TestNames(t *testing.T) {
	cases := []struct {
		name  string
		items []tstItem
		want  []string
	}{
		{"nil", nil, []string{}},
		{"empty", []tstItem{}, []string{}},
		{"all unnamed", []tstItem{{}, {}}, []string{}},
		{"unnamed skipped, order kept", []tstItem{{A: 2}, {}, {A: 1}}, []string{"n2", "n1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tstEqual(t, fmt.Sprintf("Names(%v)", c.items), parse.Names(c.items, tstName), c.want)
		})
	}
}

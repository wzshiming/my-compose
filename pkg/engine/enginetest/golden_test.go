package enginetest_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// tstStepRender scripts one step type's Render result and the block RenderSteps must print for it.
type tstStepRender struct {
	argv  []string
	warns []plan.Warning
	err   error
	block string
}

// tstStepRenders covers a plain argv, warnings kept in the returned order, and an error; every other step type renders nil, nil, nil.
var tstStepRenders = map[string]tstStepRender{
	"StartContainers": {
		argv:  []string{"start", "a"},
		block: "StartContainers\n  argv: [\"start\" \"a\"]\n  warnings:\n",
	},
	"RunContainer": {
		argv: []string{"run", "a"},
		warns: []plan.Warning{
			{Scope: "services.web", Attribute: "volumes", Message: "ignored (b)"},
			{Scope: "services.web", Attribute: "ports", Message: "ignored (a)"},
		},
		block: "RunContainer\n  argv: [\"run\" \"a\"]\n  warnings:\n    services.web: volumes: ignored (b)\n    services.web: ports: ignored (a)\n",
	},
	"BuildImage": {
		err:   errors.New("boom"),
		block: "BuildImage\n  error: boom\n  argv: []\n  warnings:\n",
	},
}

func TestRenderSteps(t *testing.T) {
	f := enginetest.NewFake("x")
	f.RenderFn = func(s plan.Step) ([]string, []plan.Warning, error) {
		r := tstStepRenders[typeName(s)]
		return r.argv, r.warns, r.err
	}
	got := enginetest.RenderSteps(f)

	t.Run("renders every step in Steps order", func(t *testing.T) {
		if want := enginetest.Steps(); !reflect.DeepEqual(f.Rendered, want) {
			t.Errorf("Render was called with %d step(s), want the %d of Steps() in order", len(f.Rendered), len(want))
		}
	})
	t.Run("one block per step in Steps order", func(t *testing.T) {
		var want strings.Builder
		for _, s := range enginetest.Steps() {
			name := typeName(s)
			if r, ok := tstStepRenders[name]; ok {
				want.WriteString(r.block)
			} else {
				want.WriteString(name + "\n  argv: []\n  warnings:\n")
			}
		}
		if got != want.String() {
			t.Errorf("RenderSteps() =\n%q\nwant\n%q", got, want.String())
		}
	})
	for name, r := range tstStepRenders {
		t.Run(name+" block", func(t *testing.T) {
			if !strings.Contains(got, r.block) {
				t.Errorf("RenderSteps() =\n%q\nwant it to contain\n%q", got, r.block)
			}
		})
	}
	t.Run("no blank lines and one trailing newline", func(t *testing.T) {
		if got == "" || !strings.HasSuffix(got, "\n") {
			t.Errorf("RenderSteps() = %q, want it to end with a newline", got)
		}
		if strings.HasPrefix(got, "\n") || strings.Contains(got, "\n\n") {
			t.Errorf("RenderSteps() = %q, want no blank lines", got)
		}
	})
}

// tstWriteGolden writes content to a steps.txt in a fresh temp dir and returns its path.
func tstWriteGolden(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "steps.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGolden(t *testing.T) {
	const content = "ListImages\n  argv: [\"image\" \"ls\"]\n  warnings:\n"

	t.Run("missing golden reports once and writes nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "steps.txt")
		rec := &recordingTB{TB: t}
		enginetest.Golden(rec, path, content, false)
		if len(rec.errs) != 1 {
			t.Fatalf("Golden(missing) reported %d error(s), want 1: %q", len(rec.errs), rec.errs)
		}
		if msg := rec.errs[0]; !strings.Contains(msg, path) || !strings.Contains(msg, "-update") {
			t.Errorf("Golden(missing) error = %q, want it to name %s and -update", msg, path)
		}
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Golden(missing) left %s behind (stat error = %v), want no file", path, err)
		}
	})

	t.Run("update creates the file and its directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "testdata", "steps.txt")
		rec := &recordingTB{TB: t}
		enginetest.Golden(rec, path, content, true)
		if len(rec.errs) != 0 {
			t.Errorf("Golden(update) reported %q, want no errors", rec.errs)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Golden(update) did not create %s: %v", path, err)
		}
		if string(b) != content {
			t.Errorf("Golden(update) wrote %q, want %q", b, content)
		}
		if runtime.GOOS == "windows" {
			return // no POSIX permission bits: os.WriteFile's 0o644 lands as 0666
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Errorf("Golden(update) wrote %s with mode %o, want 644", path, perm)
		}
	})

	t.Run("equal content passes", func(t *testing.T) {
		path := tstWriteGolden(t, content)
		rec := &recordingTB{TB: t}
		enginetest.Golden(rec, path, content, false)
		if len(rec.errs) != 0 {
			t.Errorf("Golden(equal) reported %q, want no errors", rec.errs)
		}
	})

	t.Run("differing content reports the first diff", func(t *testing.T) {
		path := tstWriteGolden(t, "ListNetworks\n  argv: [\"image\" \"ls\"]\n  warnings:\n")
		rec := &recordingTB{TB: t}
		enginetest.Golden(rec, path, content, false)
		if len(rec.errs) != 1 {
			t.Fatalf("Golden(differing) reported %d error(s), want 1: %q", len(rec.errs), rec.errs)
		}
		for _, want := range []string{path, "differs", "-update", "line 1:"} {
			if !strings.Contains(rec.errs[0], want) {
				t.Errorf("Golden(differing) error = %q, want it to contain %q", rec.errs[0], want)
			}
		}
	})

	t.Run("update overwrites an existing file", func(t *testing.T) {
		path := tstWriteGolden(t, "old\n")
		rec := &recordingTB{TB: t}
		enginetest.Golden(rec, path, content, true)
		if len(rec.errs) != 0 {
			t.Errorf("Golden(update existing) reported %q, want no errors", rec.errs)
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != content {
			t.Errorf("after Golden(update existing) %s = %q, %v; want %q", path, b, err, content)
		}
	})
}

func TestFirstDiff(t *testing.T) {
	tests := []struct {
		name, want, got, diff string
	}{
		{"equal", "x", "x", ""},
		{"second line differs", "a\nb\n", "a\nc\n", "line 2:\n  want: \"b\"\n  got:  \"c\""},
		{"got lacks the trailing newline", "a\n", "a", "line 2:\n  want: \"\"\n  got:  (no line)"},
		{"want lacks the trailing newline", "a", "a\n", "line 2:\n  want: (no line)\n  got:  \"\""},
		{"carriage return is visible", "a\r\n", "a\n", "line 1:\n  want: \"a\\r\"\n  got:  \"a\""},
		{"empty want", "", "z", "line 1:\n  want: \"\"\n  got:  \"z\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := enginetest.FirstDiff(tt.want, tt.got); diff != tt.diff {
				t.Errorf("FirstDiff(%q, %q) = %q, want %q", tt.want, tt.got, diff, tt.diff)
			}
		})
	}
}

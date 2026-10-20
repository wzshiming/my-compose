package enginetest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine"
)

// RenderSteps renders every Steps() fixture through e into one block per step: the step type name, an error line when Render fails, the quoted argv, and the warnings as returned.
func RenderSteps(e engine.Engine) string {
	var b strings.Builder
	for _, step := range Steps() {
		argv, warnings, err := e.Render(step)
		b.WriteString(reflect.TypeOf(step).Name() + "\n")
		if err != nil {
			b.WriteString("  error: " + err.Error() + "\n")
		}
		fmt.Fprintf(&b, "  argv: %q\n  warnings:\n", argv)
		for _, w := range warnings {
			b.WriteString("    " + w.String() + "\n")
		}
	}
	return b.String()
}

// Golden rewrites the file at path with got when update is set, and otherwise reports through tb.Errorf when the file is missing or differs from got.
func Golden(tb testing.TB, path, got string, update bool) {
	tb.Helper()
	if update {
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err == nil {
			err = os.WriteFile(path, []byte(got), 0o644)
		}
		if err != nil {
			tb.Errorf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		tb.Errorf("missing golden %s; run go test -update to create it", path)
	case err != nil:
		tb.Errorf("read golden: %v", err)
	case string(want) != got:
		tb.Errorf("%s differs; run go test -update after reviewing:\n%s", path, FirstDiff(string(want), got))
	}
}

// FirstDiff describes the first line on which want and got differ, quoting each side or naming its missing line, and returns "" when they are equal.
func FirstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(w), len(g)) {
		wl, gl := "(no line)", "(no line)"
		if i < len(w) {
			wl = strconv.Quote(w[i])
		}
		if i < len(g) {
			gl = strconv.Quote(g[i])
		}
		if wl != gl {
			return fmt.Sprintf("line %d:\n  want: %s\n  got:  %s", i+1, wl, gl)
		}
	}
	return ""
}

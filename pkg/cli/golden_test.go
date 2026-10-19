package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tstUpdate = flag.Bool("update", false, "rewrite golden files")

var tstGoldenEngines = []string{"docker", "podman", "nerdctl", "wslc", "container"}

// tstGoldenCases pairs a testdata directory with the service its up-service command starts.
var tstGoldenCases = []struct {
	dir     string
	service string
}{
	{"web", "db"},
	{"scale", "debug"},
	{"build", "app"},
}

// TestGolden pins the dry-run command lines of every engine for each testdata case; -update rewrites testdata/<case>/<engine>/<command>.golden.
func TestGolden(t *testing.T) {
	for _, c := range tstGoldenCases {
		dir, err := filepath.Abs(filepath.Join("testdata", c.dir))
		if err != nil {
			t.Fatal(err)
		}
		commands := []struct {
			name string
			args []string
		}{
			{"up", []string{"up"}},
			{"up-service", []string{"up", c.service}},
			{"down", []string{"down"}},
			{"down-v", []string{"down", "-v"}},
			{"logs", []string{"logs"}},
			{"ps", []string{"ps"}},
		}
		for _, eng := range tstGoldenEngines {
			for _, cmd := range commands {
				t.Run(c.dir+"/"+eng+"/"+cmd.name, func(t *testing.T) {
					tstIsolateEnv(t)
					args := append([]string{"--dry-run", "--engine", eng, "-p", c.dir, "--project-directory", dir, "-f", filepath.Join(dir, "compose.yaml")}, cmd.args...)
					var out, errb bytes.Buffer
					if code := Main(context.Background(), args, &out, &errb); code != 0 {
						t.Fatalf("%q: exit %d\nstdout:\n%s\nstderr:\n%s", args, code, out.String(), errb.String())
					}
					content := out.String()
					for _, line := range tstLines(errb.String()) {
						content += "# stderr: " + line + "\n"
					}
					content = strings.ReplaceAll(content, dir, "<DIR>")

					golden := filepath.Join(dir, eng, cmd.name+".golden")
					if *tstUpdate {
						if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(golden, []byte(content), 0o644); err != nil {
							t.Fatal(err)
						}
						return
					}
					want, err := os.ReadFile(golden)
					if errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("missing golden %s; run with -update", golden)
					}
					if err != nil {
						t.Fatal(err)
					}
					if string(want) != content {
						t.Errorf("%s differs from the dry run; run go test ./pkg/cli -update after reviewing:\n%s", golden, tstFirstDiff(string(want), content))
					}
				})
			}
		}
	}
}

func tstFirstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d:\n  golden: %s\n  output: %s", i+1, wl, gl)
		}
	}
	return "no differing line (trailing whitespace?)"
}

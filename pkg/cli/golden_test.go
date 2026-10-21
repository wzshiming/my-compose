package cli

import (
	"bytes"
	"context"
	"flag"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
)

var tstUpdate = flag.Bool("update", false, "rewrite golden files")

// tstQuotedDir matches a dry-run token that runner.Quote single-quoted only because the native testdata path held backslashes.
var tstQuotedDir = regexp.MustCompile(`'([^'\n]*<DIR>[^'\n]*)'`)

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

// TestGolden pins the dry-run command lines of every engine for each testdata case; -update rewrites testdata/<case>/<engine>/<command>.txt.
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
					content = strings.ReplaceAll(filepath.ToSlash(content), filepath.ToSlash(dir), "<DIR>")
					if filepath.Separator != '/' {
						content = tstQuotedDir.ReplaceAllString(content, "$1")
					}
					enginetest.Golden(t, filepath.Join(dir, eng, cmd.name+".txt"), content, *tstUpdate)
				})
			}
		}
	}
}

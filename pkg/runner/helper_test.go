package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine/enginetest"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// tstHelperEnv marks this test binary re-executed as the child of the real os/exec tests.
const tstHelperEnv = "GO_TEST_HELPER_PROCESS"

// tstTimeout bounds every wait so a wrong implementation fails instead of hanging.
const tstTimeout = 5 * time.Second

func TestMain(m *testing.M) {
	if os.Getenv(tstHelperEnv) == "1" {
		os.Exit(tstHelperMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// tstHelperMain is the child process: "echo-exit <code> <out> <err>" writes out and err then exits with code; "sleep" idles until killed.
func tstHelperMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "tst helper: missing mode")
		return 2
	}
	switch args[0] {
	case "echo-exit":
		if len(args) != 4 {
			fmt.Fprintln(os.Stderr, "tst helper: usage: echo-exit <code> <out> <err>")
			return 2
		}
		code, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "tst helper:", err)
			return 2
		}
		os.Stdout.WriteString(args[2])
		os.Stderr.WriteString(args[3])
		return code
	case "sleep":
		time.Sleep(30 * time.Second)
		return 0
	}
	fmt.Fprintf(os.Stderr, "tst helper: unknown mode %q\n", args[0])
	return 2
}

// tstExecutable is the path of this test binary, re-run as the helper process.
func tstExecutable(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}

// tstExitError runs the helper process so it exits with code and returns the real *exec.ExitError.
func tstExitError(t *testing.T, code int) *exec.ExitError {
	t.Helper()
	cmd := exec.Command(tstExecutable(t), "echo-exit", strconv.Itoa(code), "", "")
	cmd.Env = append(os.Environ(), tstHelperEnv+"=1")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != code {
		t.Fatalf("helper exit %d: got %v, want *exec.ExitError with that code", code, err)
	}
	return exitErr
}

// tstCall is one Runner.run invocation.
type tstCall struct {
	Argv           []string
	Stdout, Stderr io.Writer
	Start          time.Time
	CtxErr         error // ctx.Err() observed when the call returned
}

// tstBehavior scripts a command given the writers the Runner handed over.
type tstBehavior func(ctx context.Context, stdout, stderr io.Writer) error

// tstRecorder stands in for Runner.run: it records every call and plays the behavior scripted for its command line; safe for concurrent use.
type tstRecorder struct {
	mu    sync.Mutex
	calls []tstCall
	on    map[string]tstBehavior
}

func tstNewRecorder() *tstRecorder {
	return &tstRecorder{on: map[string]tstBehavior{}}
}

// On scripts the command whose argv joins to cmdline, such as "fake RunContainer proj-web-1"; unscripted commands succeed silently.
func (rec *tstRecorder) On(cmdline string, b tstBehavior) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.on[cmdline] = b
}

func (rec *tstRecorder) run(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	rec.mu.Lock()
	i := len(rec.calls)
	rec.calls = append(rec.calls, tstCall{Argv: slices.Clone(argv), Stdout: stdout, Stderr: stderr, Start: time.Now()})
	b := rec.on[strings.Join(argv, " ")]
	rec.mu.Unlock()

	var err error
	if b != nil {
		err = b(ctx, stdout, stderr)
	}

	rec.mu.Lock()
	rec.calls[i].CtxErr = ctx.Err()
	rec.mu.Unlock()
	return err
}

func (rec *tstRecorder) Calls() []tstCall {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return slices.Clone(rec.calls)
}

func (rec *tstRecorder) Count() int { return len(rec.Calls()) }

// Cmdlines lists the recorded command lines in call order.
func (rec *tstRecorder) Cmdlines() []string {
	calls := rec.Calls()
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = strings.Join(c.Argv, " ")
	}
	return out
}

// WaitCalls blocks until n calls were recorded, failing t if abort closes or tstTimeout passes first.
func (rec *tstRecorder) WaitCalls(t *testing.T, n int, abort <-chan struct{}) {
	t.Helper()
	deadline := time.After(tstTimeout)
	for rec.Count() < n {
		select {
		case <-abort:
			t.Fatalf("Run returned after %d call(s), want to observe %d", rec.Count(), n)
		case <-deadline:
			t.Fatalf("recorded %d call(s) after %v, want %d", rec.Count(), tstTimeout, n)
		case <-time.After(time.Millisecond):
		}
	}
}

func tstFail(err error) tstBehavior {
	return func(context.Context, io.Writer, io.Writer) error { return err }
}

// tstWrite writes out to stdout and errOut to stderr, then succeeds.
func tstWrite(out, errOut string) tstBehavior {
	return func(_ context.Context, stdout, stderr io.Writer) error {
		io.WriteString(stdout, out)
		io.WriteString(stderr, errOut)
		return nil
	}
}

// tstBlock waits for ctx to end, like a process that only dies when signalled.
func tstBlock() tstBehavior {
	return func(ctx context.Context, _, _ io.Writer) error {
		<-ctx.Done()
		return ctx.Err()
	}
}

// tstBarrier releases its parties once n have arrived, or errors after tstTimeout so a serial implementation fails instead of hanging.
type tstBarrier struct {
	mu      sync.Mutex
	pending int
	release chan struct{}
}

func tstNewBarrier(n int) *tstBarrier {
	return &tstBarrier{pending: n, release: make(chan struct{})}
}

func (b *tstBarrier) Wait(ctx context.Context) error {
	b.mu.Lock()
	b.pending--
	if b.pending == 0 {
		close(b.release)
	}
	b.mu.Unlock()
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(tstTimeout):
		return errors.New("tst barrier: not every party arrived")
	}
}

func tstFake() *enginetest.Fake { return enginetest.NewFake("fake") }

// tstRunner builds a Runner over f whose commands rec plays (a nil rec keeps the real os/exec), with buffered Stdout/Stderr and a 1ms wait interval.
func tstRunner(f *enginetest.Fake, rec *tstRecorder) (r *Runner, stdout, stderr *bytes.Buffer) {
	stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}
	r = &Runner{Engine: f, Stdout: stdout, Stderr: stderr, interval: time.Millisecond}
	if rec != nil {
		r.run = rec.run
	}
	return r, stdout, stderr
}

// tstRunResult delivers the outcome of a Runner.Run started in the background.
type tstRunResult struct {
	done chan struct{}
	err  error
}

func tstRunAsync(ctx context.Context, r *Runner, p *plan.Plan) *tstRunResult {
	x := &tstRunResult{done: make(chan struct{})}
	go func() {
		x.err = r.Run(ctx, p, nil)
		close(x.done)
	}()
	return x
}

// Wait returns Run's error, failing t if it has not returned within tstTimeout.
func (x *tstRunResult) Wait(t *testing.T) error {
	t.Helper()
	select {
	case <-x.done:
		return x.err
	case <-time.After(tstTimeout):
		t.Fatalf("Run did not return within %v", tstTimeout)
		return nil
	}
}

func tstPlan(steps ...plan.Step) *plan.Plan { return &plan.Plan{Steps: steps} }

func tstRunStep(name string) plan.RunContainer {
	return plan.RunContainer{Container: plan.ContainerSpec{Name: name}}
}

func tstNetStep(name string) plan.CreateNetwork {
	return plan.CreateNetwork{Network: plan.NetworkSpec{Name: name}}
}

func tstState(status, health string, exitCode int) plan.ContainerState {
	return plan.ContainerState{Status: status, Health: health, ExitCode: exitCode}
}

// tstLines splits buffered output into lines, dropping the empty tail after a final newline.
func tstLines(b *bytes.Buffer) []string {
	s := b.String()
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// tstWarningLines renders ws the way Run must print them to Stderr.
func tstWarningLines(ws []plan.Warning) string {
	var b strings.Builder
	for _, w := range plan.NormalizeWarnings(ws) {
		b.WriteString("warning: " + w.String() + "\n")
	}
	return b.String()
}

// tstStepNames describes steps compactly for failure messages.
func tstStepNames(steps []plan.Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		switch s := s.(type) {
		case plan.CreateNetwork:
			out[i] = "CreateNetwork " + s.Network.Name
		case plan.RunContainer:
			out[i] = "RunContainer " + s.Container.Name
		case plan.WaitContainer:
			out[i] = "WaitContainer " + s.Name + " " + string(s.Condition)
		case plan.InspectContainer:
			out[i] = "InspectContainer " + s.Name
		case plan.Logs:
			out[i] = "Logs " + s.Name
		default:
			out[i] = fmt.Sprintf("%T", s)
		}
	}
	return out
}

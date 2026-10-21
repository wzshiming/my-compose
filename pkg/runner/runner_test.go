package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wzshiming/my-compose/pkg/plan"
)

const tstProbeDB = "fake InspectContainer proj-db-1"

var (
	tstWaitDB   = plan.WaitContainer{Name: "proj-db-1", Condition: plan.ConditionHealthy}
	tstStarting = tstState(plan.StatusRunning, plan.HealthStarting, 0)
	tstHealthy  = tstState(plan.StatusRunning, plan.HealthHealthy, 0)
	tstWarnings = []plan.Warning{
		{Scope: "services.web", Attribute: "restart", Message: "ignored (no restart support)"},
		{Scope: "services.db", Attribute: "cpus", Message: "ignored (unsupported)"},
	}
	tstRenderedW = plan.Warning{Scope: "services.web", Attribute: "init", Message: "ignored (unsupported)"}
)

func TestAC1RenderError(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("DryRun=%v", dry), func(t *testing.T) {
			f, rec := tstFake(), tstNewRecorder()
			boom := errors.New("render boom")
			def := tstFake()
			f.RenderFn = func(s plan.Step) ([]string, []plan.Warning, error) {
				if _, ok := s.(plan.RunContainer); ok {
					return nil, nil, boom
				}
				return def.Render(s)
			}
			r, stdout, stderr := tstRunner(f, rec)
			r.DryRun = dry
			p := tstPlan(tstNetStep("proj_front"), tstRunStep("proj-web-1"), plan.StartContainers{Names: []string{"proj-web-1"}})
			err := r.Run(t.Context(), p, tstWarnings)
			if !errors.Is(err, boom) {
				t.Fatalf("Run() error = %v, want errors.Is %v", err, boom)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("Run() wrote stdout %q, stderr %q; want nothing, not even warnings", stdout, stderr)
			}
			if n := rec.Count(); n != 0 {
				t.Errorf("Run() executed %d command(s), want 0", n)
			}
		})
	}
}

func TestAC1NilPlan(t *testing.T) {
	f, rec := tstFake(), tstNewRecorder()
	r, stdout, stderr := tstRunner(f, rec)
	if err := r.Run(t.Context(), nil, nil); err != nil {
		t.Fatalf("Run(nil plan) error = %v, want nil", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 || rec.Count() != 0 {
		t.Errorf("Run(nil plan) wrote stdout %q, stderr %q and executed %d command(s); want nothing", stdout, stderr, rec.Count())
	}
}

func TestAC2Warnings(t *testing.T) {
	restart, cpus := tstWarnings[0], tstWarnings[1]
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("DryRun=%v", dry), func(t *testing.T) {
			f, rec := tstFake(), tstNewRecorder()
			def := tstFake()
			f.RenderFn = func(s plan.Step) ([]string, []plan.Warning, error) {
				argv, _, err := def.Render(s)
				switch s.(type) {
				case plan.CreateNetwork:
					return argv, []plan.Warning{tstRenderedW}, err
				case plan.RunContainer:
					return argv, []plan.Warning{restart}, err // duplicates a planner warning
				}
				return argv, nil, err
			}
			r, _, stderr := tstRunner(f, rec)
			r.DryRun = dry
			if err := r.Run(t.Context(), tstPlan(tstNetStep("proj_front"), tstRunStep("proj-web-1")), []plan.Warning{restart, cpus}); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			want := tstWarningLines([]plan.Warning{restart, cpus, tstRenderedW, restart})
			if strings.Count(want, "\n") != 3 {
				t.Fatalf("fixture: want 3 distinct warnings, got %q", want)
			}
			if got := stderr.String(); got != want {
				t.Errorf("stderr = %q, want %q", got, want)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		r, _, stderr := tstRunner(f, rec)
		if err := r.Run(t.Context(), tstPlan(tstRunStep("proj-web-1")), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr)
		}
	})
}

func TestAC3Strict(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("DryRun=%v", dry), func(t *testing.T) {
			f, rec := tstFake(), tstNewRecorder()
			def := tstFake()
			f.RenderFn = func(s plan.Step) ([]string, []plan.Warning, error) {
				argv, _, err := def.Render(s)
				return argv, []plan.Warning{tstRenderedW}, err
			}
			r, stdout, stderr := tstRunner(f, rec)
			r.DryRun, r.Strict = dry, true
			err := r.Run(t.Context(), tstPlan(tstRunStep("proj-web-1")), tstWarnings)
			if err == nil || !strings.Contains(err.Error(), "strict") || !strings.Contains(err.Error(), "3") {
				t.Fatalf("Run() error = %v, want one mentioning strict and the 3 warnings", err)
			}
			if want := tstWarningLines(slices.Concat(tstWarnings, []plan.Warning{tstRenderedW})); !strings.Contains(stderr.String(), want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if n := rec.Count(); n != 0 {
				t.Errorf("Run() executed %d command(s), want 0", n)
			}
		})
	}
	t.Run("no warnings", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		r, _, stderr := tstRunner(f, rec)
		r.Strict = true
		if err := r.Run(t.Context(), tstPlan(tstRunStep("proj-web-1")), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if stderr.Len() != 0 || rec.Count() != 1 {
			t.Errorf("stderr = %q and %d command(s) executed; want empty stderr and 1 command", stderr, rec.Count())
		}
	})
}

func TestAC4DryRun(t *testing.T) {
	t.Run("one line per step", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		r, stdout, stderr := tstRunner(f, rec)
		r.DryRun = true
		p := tstPlan(
			tstNetStep("proj_front"),
			tstRunStep("proj-web-1"),
			tstWaitDB,
			plan.StartContainers{Names: []string{"proj-web-1", "proj-db-1"}},
			plan.Logs{Name: "a"},
			plan.Logs{Name: "b"},
		)
		if err := r.Run(t.Context(), p, nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		want := []string{
			"fake CreateNetwork proj_front",
			"fake RunContainer proj-web-1",
			"# wait for proj-db-1: healthy",
			"fake StartContainers proj-web-1 proj-db-1",
			"fake Logs a",
			"fake Logs b",
		}
		if got := tstLines(stdout); !reflect.DeepEqual(got, want) {
			t.Errorf("stdout lines = %q, want %q", got, want)
		}
		if stderr.Len() != 0 {
			t.Errorf("stderr = %q, want empty", stderr)
		}
		if n := rec.Count(); n != 0 {
			t.Errorf("dry run executed %d command(s), want 0", n)
		}
		for method, inputs := range f.ParsedInputs {
			if len(inputs) != 0 {
				t.Errorf("dry run called %s %d time(s), want 0", method, len(inputs))
			}
		}
	})
	t.Run("query steps print too", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		r, stdout, _ := tstRunner(f, rec)
		r.DryRun = true
		p := tstPlan(plan.ListContainers{Project: "proj"}, plan.InspectContainer{Name: "x"}, plan.ListNetworks{})
		if err := r.Run(t.Context(), p, nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		want := []string{"fake ListContainers proj", "fake InspectContainer x", "fake ListNetworks"}
		if got := tstLines(stdout); !reflect.DeepEqual(got, want) {
			t.Errorf("stdout lines = %q, want %q", got, want)
		}
		if n := rec.Count(); n != 0 {
			t.Errorf("dry run executed %d command(s), want 0", n)
		}
	})
	t.Run("quotes unsafe tokens", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		f.RenderFn = func(plan.Step) ([]string, []plan.Warning, error) {
			return []string{"run", "--label", "a b"}, nil, nil
		}
		r, stdout, _ := tstRunner(f, rec)
		r.DryRun = true
		if err := r.Run(t.Context(), tstPlan(tstRunStep("proj-web-1")), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if got, want := stdout.String(), "fake run --label 'a b'\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})
}

func TestAC6Order(t *testing.T) {
	p := tstPlan(tstNetStep("proj_front"), tstRunStep("proj-web-1"), plan.StartContainers{Names: []string{"proj-web-1"}})
	want := []string{"fake CreateNetwork proj_front", "fake RunContainer proj-web-1", "fake StartContainers proj-web-1"}
	t.Run("plan order", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		r, _, _ := tstRunner(f, rec)
		if err := r.Run(t.Context(), p, nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, want) {
			t.Errorf("executed %q, want %q", got, want)
		}
	})
	t.Run("first failure stops", func(t *testing.T) {
		exitErr := tstExitError(t, 3)
		f, rec := tstFake(), tstNewRecorder()
		rec.On(want[1], tstFail(exitErr))
		r, _, _ := tstRunner(f, rec)
		err := r.Run(t.Context(), p, nil)
		if err == nil || !strings.Contains(err.Error(), want[1]) || !strings.Contains(err.Error(), "exit status 3") {
			t.Fatalf("Run() error = %v, want it to mention %q and exit status 3", err, want[1])
		}
		var got *exec.ExitError
		if !errors.As(err, &got) || got.ExitCode() != 3 {
			t.Errorf("errors.As(*exec.ExitError) failed on %v", err)
		}
		if cmds := rec.Cmdlines(); !reflect.DeepEqual(cmds, want[:2]) {
			t.Errorf("executed %q, want %q (the third step must not start)", cmds, want[:2])
		}
	})
}

func TestAC7Streams(t *testing.T) {
	f, rec := tstFake(), tstNewRecorder()
	r, stdout, stderr := tstRunner(f, rec)
	if err := r.Run(t.Context(), tstPlan(tstRunStep("proj-web-1")), nil); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	calls := rec.Calls()
	if len(calls) != 1 {
		t.Fatalf("executed %q, want exactly one command", rec.Cmdlines())
	}
	if calls[0].Stdout != r.Stdout || calls[0].Stderr != r.Stderr {
		t.Errorf("run got stdout %p, stderr %p; want r.Stdout %p and r.Stderr %p unchanged", calls[0].Stdout, calls[0].Stderr, stdout, stderr)
	}
}

func TestAC8Cancel(t *testing.T) {
	t.Run("blocked step", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		rec.On("fake RunContainer proj-a-1", tstBlock())
		r, _, _ := tstRunner(f, rec)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		res := tstRunAsync(ctx, r, tstPlan(tstRunStep("proj-a-1"), tstRunStep("proj-b-1")))
		rec.WaitCalls(t, 1, res.done)
		cancel()
		if err := res.Wait(t); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{"fake RunContainer proj-a-1"}) {
			t.Errorf("executed %q, want only the blocked step", got)
		}
	})
	t.Run("between polls", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		f.Inspects = []plan.ContainerState{tstStarting}
		rec.On(tstProbeDB, tstWrite("{}", ""))
		r, _, _ := tstRunner(f, rec)
		r.interval = time.Hour
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		res := tstRunAsync(ctx, r, tstPlan(tstWaitDB, tstRunStep("proj-web-1")))
		rec.WaitCalls(t, 1, res.done)
		cancel()
		if err := res.Wait(t); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{tstProbeDB}) {
			t.Errorf("executed %q, want the single probe before cancellation", got)
		}
	})
	t.Run("already canceled", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		r, _, _ := tstRunner(f, rec)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := r.Run(ctx, tstPlan(tstRunStep("proj-web-1")), nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
		if n := rec.Count(); n != 0 {
			t.Errorf("executed %d command(s) on a canceled context, want 0", n)
		}
	})
}

func TestAC9DefaultRun(t *testing.T) {
	t.Setenv(tstHelperEnv, "1")
	exe := tstExecutable(t)
	t.Run("streams and exit status", func(t *testing.T) {
		f := tstFake()
		f.BinaryV = exe
		f.RenderFn = func(plan.Step) ([]string, []plan.Warning, error) {
			return []string{"echo-exit", "3", "out-text", "err-text"}, nil, nil
		}
		r, stdout, stderr := tstRunner(f, nil)
		err := r.Run(t.Context(), tstPlan(tstRunStep("proj-web-1")), nil)
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
			t.Fatalf("Run() error = %v, want a wrapped *exec.ExitError with code 3", err)
		}
		if !strings.Contains(err.Error(), "echo-exit 3 out-text err-text") {
			t.Errorf("Run() error = %q, want it to quote the command line", err)
		}
		if stdout.String() != "out-text" || stderr.String() != "err-text" {
			t.Errorf("child output reached stdout %q, stderr %q; want out-text and err-text", stdout, stderr)
		}
	})
	t.Run("missing executable", func(t *testing.T) {
		f := tstFake()
		f.BinaryV = "tst-no-such-binary"
		r, _, _ := tstRunner(f, nil)
		err := r.Run(t.Context(), tstPlan(tstRunStep("proj-web-1")), nil)
		if err == nil || !strings.Contains(err.Error(), "tst-no-such-binary RunContainer proj-web-1") {
			t.Fatalf("Run() error = %v, want it to name the command line", err)
		}
		if !errors.Is(err, exec.ErrNotFound) {
			t.Errorf("Run() error = %v, want errors.Is exec.ErrNotFound", err)
		}
	})
	t.Run("cancel terminates the child", func(t *testing.T) {
		f := tstFake()
		f.BinaryV = exe
		f.RenderFn = func(plan.Step) ([]string, []plan.Warning, error) {
			return []string{"sleep"}, nil, nil
		}
		r, _, _ := tstRunner(f, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		time.AfterFunc(200*time.Millisecond, cancel)
		res := tstRunAsync(ctx, r, tstPlan(tstRunStep("proj-web-1")))
		select {
		case <-res.done:
		case <-time.After(6 * time.Second):
			t.Fatal("Run did not return within 6s of cancellation")
		}
		if !errors.Is(res.err, context.Canceled) {
			t.Errorf("Run() error = %v, want context.Canceled", res.err)
		}
	})
}

func TestAC10WaitHealthy(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		f.Inspects = []plan.ContainerState{tstStarting, tstStarting, tstHealthy}
		rec.On(tstProbeDB, tstWrite("{}", ""))
		r, stdout, _ := tstRunner(f, rec)
		if err := r.Run(t.Context(), tstPlan(tstWaitDB), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{tstProbeDB, tstProbeDB, tstProbeDB}) {
			t.Errorf("executed %q, want three probes", got)
		}
		for i, c := range rec.Calls() {
			if c.Stdout == r.Stdout {
				t.Errorf("probe %d wrote to r.Stdout, want its stdout captured", i)
			}
			if c.Stderr != r.Stderr {
				t.Errorf("probe %d got stderr %p, want r.Stderr passed through", i, c.Stderr)
			}
		}
		if got := f.ParsedInputs["ParseInspect"]; len(got) != 3 || string(got[0]) != "{}" || string(got[2]) != "{}" {
			t.Errorf("ParseInspect inputs = %q, want the captured {} three times", got)
		}
		if stdout.Len() != 0 {
			t.Errorf("stdout = %q, want probe output kept off r.Stdout", stdout)
		}
	})
	t.Run("completed", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		f.Inspects = []plan.ContainerState{tstState(plan.StatusRunning, plan.HealthNone, 0), tstState(plan.StatusExited, plan.HealthNone, 0)}
		rec.On(tstProbeDB, tstWrite("{}", ""))
		r, _, _ := tstRunner(f, rec)
		if err := r.Run(t.Context(), tstPlan(plan.WaitContainer{Name: "proj-db-1", Condition: plan.ConditionCompleted}), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{tstProbeDB, tstProbeDB}) {
			t.Errorf("executed %q, want two probes", got)
		}
	})
}

func TestAC11WaitFailures(t *testing.T) {
	tests := []struct {
		name  string
		cond  plan.Condition
		state plan.ContainerState
		want  string
	}{
		{"healthy exited", plan.ConditionHealthy, tstState(plan.StatusExited, plan.HealthNone, 1), "exited with code 1"},
		{"healthy unhealthy", plan.ConditionHealthy, tstState(plan.StatusRunning, plan.HealthUnhealthy, 0), "unhealthy"},
		{"healthy no healthcheck", plan.ConditionHealthy, tstState(plan.StatusRunning, plan.HealthNone, 0), "no healthcheck"},
		{"healthy dead", plan.ConditionHealthy, tstState(plan.StatusDead, plan.HealthNone, 0), "dead"},
		{"completed exited non-zero", plan.ConditionCompleted, tstState(plan.StatusExited, plan.HealthNone, 2), "code 2"},
		{"completed dead", plan.ConditionCompleted, tstState(plan.StatusDead, plan.HealthNone, 0), "dead"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, rec := tstFake(), tstNewRecorder()
			f.Inspects = []plan.ContainerState{tt.state}
			rec.On(tstProbeDB, tstWrite("{}", ""))
			r, _, _ := tstRunner(f, rec)
			err := r.Run(t.Context(), tstPlan(plan.WaitContainer{Name: "proj-db-1", Condition: tt.cond}, tstRunStep("proj-web-1")), nil)
			if err == nil {
				t.Fatal("Run() error = nil, want a failed wait")
			}
			for _, want := range []string{"wait for proj-db-1 (" + string(tt.cond) + ")", tt.want} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Run() error = %q, want it to contain %q", err, want)
				}
			}
			if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{tstProbeDB}) {
				t.Errorf("executed %q, want a single probe and no further step", got)
			}
		})
	}
}

func TestAC12ProbeFailure(t *testing.T) {
	t.Run("probe exits non-zero", func(t *testing.T) {
		exitErr := tstExitError(t, 1)
		f, rec := tstFake(), tstNewRecorder()
		f.Inspects = []plan.ContainerState{tstHealthy}
		rec.On(tstProbeDB, tstFail(exitErr))
		r, _, _ := tstRunner(f, rec)
		err := r.Run(t.Context(), tstPlan(tstWaitDB, tstRunStep("proj-web-1")), nil)
		if err == nil || !strings.Contains(err.Error(), tstProbeDB) || !strings.Contains(err.Error(), "exit status 1") {
			t.Fatalf("Run() error = %v, want it to name the probe command line and exit status 1", err)
		}
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{tstProbeDB}) {
			t.Errorf("executed %q, want a single probe without retry", got)
		}
	})
	t.Run("parse error", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		f.Inspects = []plan.ContainerState{tstHealthy}
		f.ParseErr = errors.New("bad inspect")
		rec.On(tstProbeDB, tstWrite("{}", ""))
		r, _, _ := tstRunner(f, rec)
		err := r.Run(t.Context(), tstPlan(tstWaitDB, tstRunStep("proj-web-1")), nil)
		if !errors.Is(err, f.ParseErr) {
			t.Fatalf("Run() error = %v, want errors.Is %v", err, f.ParseErr)
		}
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{tstProbeDB}) {
			t.Errorf("executed %q, want a single probe without retry", got)
		}
	})
}

func TestAC13Interval(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		f.Inspects = []plan.ContainerState{tstStarting, tstStarting, tstHealthy}
		rec.On(tstProbeDB, tstWrite("{}", ""))
		r, _, _ := tstRunner(f, rec)
		r.interval = 20 * time.Millisecond
		start := time.Now()
		if err := r.Run(t.Context(), tstPlan(tstWaitDB), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		elapsed := time.Since(start)
		calls := rec.Calls()
		if len(calls) != 3 {
			t.Fatalf("executed %q, want three probes", rec.Cmdlines())
		}
		for i := 1; i < len(calls); i++ {
			if gap := calls[i].Start.Sub(calls[i-1].Start); gap < 20*time.Millisecond {
				t.Errorf("probes %d and %d are %v apart, want >= 20ms", i-1, i, gap)
			}
		}
		if elapsed > time.Second {
			t.Errorf("Run took %v with a 20ms interval, want well under the 1s default", elapsed)
		}
	})
	t.Run("default", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		f.Inspects = []plan.ContainerState{tstStarting, tstHealthy}
		rec.On(tstProbeDB, tstWrite("{}", ""))
		r := &Runner{Engine: f, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, run: rec.run}
		if err := r.Run(t.Context(), tstPlan(tstWaitDB), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		calls := rec.Calls()
		if len(calls) != 2 {
			t.Fatalf("executed %q, want two probes", rec.Cmdlines())
		}
		if gap := calls[1].Start.Sub(calls[0].Start); gap < 900*time.Millisecond {
			t.Errorf("probes are %v apart with the zero interval, want the 1s default", gap)
		}
	})
}

func TestAC14LogsGroup(t *testing.T) {
	t.Run("single passes through", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		rec.On("fake Logs web-1", tstWrite("hello\n", "oops\n"))
		r, stdout, stderr := tstRunner(f, rec)
		if err := r.Run(t.Context(), tstPlan(plan.Logs{Name: "web-1"}), nil); err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
		calls := rec.Calls()
		if len(calls) != 1 || calls[0].Stdout != r.Stdout || calls[0].Stderr != r.Stderr {
			t.Fatalf("single Logs ran as %q with its own writers, want one call with r.Stdout/r.Stderr passed through", rec.Cmdlines())
		}
		if stdout.String() != "hello\n" || stderr.String() != "oops\n" {
			t.Errorf("stdout = %q, stderr = %q; want unprefixed hello and oops", stdout, stderr)
		}
	})
	t.Run("group prefixes whole lines", func(t *testing.T) {
		names := []string{"web-1", "db-1", "cache-1"}
		f, rec := tstFake(), tstNewRecorder()
		barrier := tstNewBarrier(len(names))
		for _, name := range names {
			rec.On("fake Logs "+name, func(ctx context.Context, stdout, stderr io.Writer) error {
				if err := barrier.Wait(ctx); err != nil {
					return err
				}
				for _, chunk := range []string{"on", "e\ntw", "o\nta", "il"} {
					io.WriteString(stdout, chunk)
				}
				io.WriteString(stderr, "oo")
				io.WriteString(stderr, "ps\n")
				return nil
			})
		}
		r, stdout, stderr := tstRunner(f, rec)
		p := tstPlan(plan.Logs{Name: "web-1"}, plan.Logs{Name: "db-1"}, plan.Logs{Name: "cache-1"})
		if err := r.Run(t.Context(), p, nil); err != nil {
			t.Fatalf("Run() error = %v, want nil (a barrier error means the group did not run concurrently)", err)
		}
		if n := rec.Count(); n != 3 {
			t.Fatalf("executed %q, want all three Logs", rec.Cmdlines())
		}
		for _, c := range rec.Calls() {
			if c.Stdout == r.Stdout || c.Stderr == r.Stderr {
				t.Errorf("%q got r.Stdout/r.Stderr directly, want prefixing writers", c.Argv)
			}
		}
		prefix := map[string]string{"web-1": "web-1   | ", "db-1": "db-1    | ", "cache-1": "cache-1 | "}
		var wantOut, wantErr []string
		for _, n := range names {
			wantOut = append(wantOut, prefix[n]+"one", prefix[n]+"two", prefix[n]+"tail")
			wantErr = append(wantErr, prefix[n]+"oops")
		}
		tstSameLines(t, "stdout", tstLines(stdout), wantOut, prefix)
		tstSameLines(t, "stderr", tstLines(stderr), wantErr, prefix)
	})
}

// tstSameLines checks got holds exactly the want lines, each prefix's lines in their want order.
func tstSameLines(t *testing.T, stream string, got, want []string, prefixes map[string]string) {
	t.Helper()
	gs, ws := slices.Clone(got), slices.Clone(want)
	slices.Sort(gs)
	slices.Sort(ws)
	if !slices.Equal(gs, ws) {
		t.Errorf("%s lines = %q, want exactly %q in any interleaving", stream, got, want)
		return
	}
	for _, p := range prefixes {
		var g, w []string
		for _, l := range got {
			if strings.HasPrefix(l, p) {
				g = append(g, l)
			}
		}
		for _, l := range want {
			if strings.HasPrefix(l, p) {
				w = append(w, l)
			}
		}
		if !slices.Equal(g, w) {
			t.Errorf("%s lines for %q = %q, want %q in order", stream, p, g, w)
		}
	}
}

func TestAC15LogsGroupErrors(t *testing.T) {
	t.Run("first error in group order without canceling the rest", func(t *testing.T) {
		errWeb, errCache := tstExitError(t, 1), tstExitError(t, 2)
		f, rec := tstFake(), tstNewRecorder()
		barrier := tstNewBarrier(3)
		rec.On("fake Logs web-1", func(ctx context.Context, _, _ io.Writer) error {
			if err := barrier.Wait(ctx); err != nil {
				return err
			}
			time.Sleep(30 * time.Millisecond)
			return errWeb
		})
		rec.On("fake Logs db-1", func(ctx context.Context, _, _ io.Writer) error {
			if err := barrier.Wait(ctx); err != nil {
				return err
			}
			time.Sleep(60 * time.Millisecond)
			return nil
		})
		rec.On("fake Logs cache-1", func(ctx context.Context, _, _ io.Writer) error {
			if err := barrier.Wait(ctx); err != nil {
				return err
			}
			return errCache
		})
		r, _, _ := tstRunner(f, rec)
		p := tstPlan(plan.Logs{Name: "web-1"}, plan.Logs{Name: "db-1"}, plan.Logs{Name: "cache-1"}, tstRunStep("proj-web-1"))
		err := r.Run(t.Context(), p, nil)
		if !errors.Is(err, errWeb) {
			t.Fatalf("Run() error = %v, want web-1's %v (first in group order, though cache-1 failed first)", err, errWeb)
		}
		calls := rec.Calls()
		if len(calls) != 3 {
			t.Fatalf("executed %q, want the three Logs and nothing after", rec.Cmdlines())
		}
		for _, c := range calls {
			if strings.Join(c.Argv, " ") == "fake Logs db-1" && c.CtxErr != nil {
				t.Errorf("db-1 saw ctx.Err() = %v when it finished, want nil: one failure must not cancel the rest", c.CtxErr)
			}
		}
	})
	t.Run("follow canceled", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		rec.On("fake Logs web-1", tstBlock())
		rec.On("fake Logs db-1", tstBlock())
		r, _, _ := tstRunner(f, rec)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		res := tstRunAsync(ctx, r, tstPlan(plan.Logs{Name: "web-1", Follow: true}, plan.Logs{Name: "db-1", Follow: true}))
		rec.WaitCalls(t, 2, res.done)
		cancel()
		if err := res.Wait(t); !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	})
	t.Run("group boundaries", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		barrier := tstNewBarrier(2)
		enter := func(ctx context.Context, _, _ io.Writer) error { return barrier.Wait(ctx) }
		rec.On("fake Logs a", enter)
		rec.On("fake Logs b", enter)
		r, _, _ := tstRunner(f, rec)
		p := tstPlan(plan.Logs{Name: "a"}, plan.Logs{Name: "b"}, tstRunStep("proj-web-1"), plan.Logs{Name: "c"})
		if err := r.Run(t.Context(), p, nil); err != nil {
			t.Fatalf("Run() error = %v, want nil (a barrier error means Logs a and b did not overlap)", err)
		}
		got := rec.Cmdlines()
		if len(got) != 4 {
			t.Fatalf("executed %q, want four commands", got)
		}
		first := []string{got[0], got[1]}
		slices.Sort(first)
		if !slices.Equal(first, []string{"fake Logs a", "fake Logs b"}) || got[2] != "fake RunContainer proj-web-1" || got[3] != "fake Logs c" {
			t.Errorf("call order = %q, want {Logs a, Logs b} then RunContainer then Logs c", got)
		}
		calls := rec.Calls()
		if calls[0].Stdout == r.Stdout || calls[1].Stdout == r.Stdout {
			t.Errorf("grouped Logs a and b got r.Stdout directly, want prefixing writers")
		}
		if calls[3].Stdout != r.Stdout {
			t.Errorf("trailing single Logs c got %p, want r.Stdout passed through", calls[3].Stdout)
		}
	})
}

func TestAC16Query(t *testing.T) {
	list := plan.ListContainers{Project: "proj"}
	t.Run("captures stdout", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		rec.On("fake ListContainers proj", tstWrite(`[{"Name":"proj-web-1"}]`, "warn\n"))
		r, stdout, stderr := tstRunner(f, rec)
		out, err := r.Query(t.Context(), list)
		if err != nil {
			t.Fatalf("Query() error = %v, want nil", err)
		}
		if string(out) != `[{"Name":"proj-web-1"}]` {
			t.Errorf("Query() = %q, want the command's stdout", out)
		}
		calls := rec.Calls()
		if got := rec.Cmdlines(); !reflect.DeepEqual(got, []string{"fake ListContainers proj"}) {
			t.Fatalf("executed %q, want the single list command", got)
		}
		if calls[0].Stdout == r.Stdout || calls[0].Stderr != r.Stderr {
			t.Errorf("query got stdout %p, stderr %p; want captured stdout and r.Stderr", calls[0].Stdout, calls[0].Stderr)
		}
		if stdout.Len() != 0 || stderr.String() != "warn\n" {
			t.Errorf("stdout = %q, stderr = %q; want empty stdout and the passed-through warn", stdout, stderr)
		}
		if !reflect.DeepEqual(f.Rendered, []plan.Step{list}) {
			t.Errorf("Rendered = %v, want just %v", tstStepNames(f.Rendered), tstStepNames([]plan.Step{list}))
		}
	})
	t.Run("allowed steps", func(t *testing.T) {
		for _, s := range []plan.Step{plan.ListNetworks{}, plan.ListVolumes{}, plan.ListImages{}, plan.InspectContainer{Name: "x"}} {
			f, rec := tstFake(), tstNewRecorder()
			r, _, _ := tstRunner(f, rec)
			if _, err := r.Query(t.Context(), s); err != nil {
				t.Errorf("Query(%T) error = %v, want nil", s, err)
			}
			if n := rec.Count(); n != 1 {
				t.Errorf("Query(%T) executed %d command(s), want 1", s, n)
			}
		}
	})
	t.Run("unsupported steps", func(t *testing.T) {
		for _, s := range []plan.Step{tstRunStep("proj-web-1"), plan.Logs{Name: "x"}, nil} {
			f, rec := tstFake(), tstNewRecorder()
			r, _, _ := tstRunner(f, rec)
			_, err := r.Query(t.Context(), s)
			if err == nil || !strings.Contains(err.Error(), "unsupported step") {
				t.Errorf("Query(%T) error = %v, want one containing %q", s, err, "unsupported step")
			}
			if n := rec.Count(); n != 0 {
				t.Errorf("Query(%T) executed %d command(s), want 0", s, n)
			}
		}
	})
	t.Run("dry run", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		r, _, _ := tstRunner(f, rec)
		r.DryRun = true
		_, err := r.Query(t.Context(), list)
		if err == nil || !strings.Contains(err.Error(), "not available in dry run") {
			t.Errorf("Query() error = %v, want one containing %q", err, "not available in dry run")
		}
		if n := rec.Count(); n != 0 {
			t.Errorf("dry-run Query executed %d command(s), want 0", n)
		}
	})
	t.Run("non-zero exit", func(t *testing.T) {
		exitErr := tstExitError(t, 2)
		f, rec := tstFake(), tstNewRecorder()
		rec.On("fake ListContainers proj", tstFail(exitErr))
		r, _, _ := tstRunner(f, rec)
		_, err := r.Query(t.Context(), list)
		if err == nil || !strings.Contains(err.Error(), "fake ListContainers proj") || !strings.Contains(err.Error(), "exit status 2") {
			t.Fatalf("Query() error = %v, want it to name the command line and exit status 2", err)
		}
		var got *exec.ExitError
		if !errors.As(err, &got) {
			t.Errorf("errors.As(*exec.ExitError) failed on %v", err)
		}
	})
}

func TestAC17Rendered(t *testing.T) {
	job := plan.WaitContainer{Name: "proj-job-1", Condition: plan.ConditionCompleted}
	p := tstPlan(tstNetStep("proj_front"), tstWaitDB, tstRunStep("proj-web-1"), job, plan.Logs{Name: "a"})
	want := []plan.Step{
		tstNetStep("proj_front"),
		tstWaitDB,
		plan.InspectContainer{Name: "proj-db-1"},
		tstRunStep("proj-web-1"),
		job,
		plan.InspectContainer{Name: "proj-job-1"},
		plan.Logs{Name: "a"},
	}
	for _, dry := range []bool{true, false} {
		t.Run(fmt.Sprintf("DryRun=%v", dry), func(t *testing.T) {
			f, rec := tstFake(), tstNewRecorder()
			f.Inspects = []plan.ContainerState{tstHealthy, tstState(plan.StatusExited, plan.HealthNone, 0)}
			rec.On(tstProbeDB, tstWrite("{}", ""))
			rec.On("fake InspectContainer proj-job-1", tstWrite("{}", ""))
			r, _, _ := tstRunner(f, rec)
			r.DryRun = dry
			if err := r.Run(t.Context(), p, nil); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(f.Rendered, want) {
				t.Errorf("Rendered = %q, want %q", tstStepNames(f.Rendered), tstStepNames(want))
			}
		})
	}
}

func TestAC18QueryCancel(t *testing.T) {
	list := plan.ListContainers{Project: "proj"}
	t.Run("blocked", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		// Like os/exec after SIGTERM: the process error wins over ctx.Err().
		rec.On("fake ListContainers proj", func(ctx context.Context, _, _ io.Writer) error {
			<-ctx.Done()
			return errors.New("signal: terminated")
		})
		r, _, _ := tstRunner(f, rec)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		res := tstQueryAsync(ctx, r, list)
		rec.WaitCalls(t, 1, res.done)
		cancel()
		out, err := res.Wait(t, tstTimeout)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Query() error = %v, want context.Canceled", err)
		}
		if out != nil {
			t.Errorf("Query() = %q, want nil bytes after cancellation", out)
		}
	})
	t.Run("real exec", func(t *testing.T) {
		t.Setenv(tstHelperEnv, "1")
		f := tstFake()
		f.BinaryV = tstExecutable(t)
		f.RenderFn = func(plan.Step) ([]string, []plan.Warning, error) {
			return []string{"sleep"}, nil, nil
		}
		r, _, _ := tstRunner(f, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		time.AfterFunc(200*time.Millisecond, cancel)
		out, err := tstQueryAsync(ctx, r, list).Wait(t, 6*time.Second)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Query() error = %v, want context.Canceled", err)
		}
		if out != nil {
			t.Errorf("Query() = %q, want nil bytes after cancellation", out)
		}
	})
	t.Run("not canceled", func(t *testing.T) {
		f, rec := tstFake(), tstNewRecorder()
		rec.On("fake ListContainers proj", tstFail(errors.New("exit status 1")))
		r, _, _ := tstRunner(f, rec)
		out, err := r.Query(t.Context(), list)
		if err == nil || !strings.Contains(err.Error(), "fake ListContainers proj") || !strings.Contains(err.Error(), "exit status 1") {
			t.Fatalf("Query() error = %v, want it to name the command line and exit status 1", err)
		}
		if errors.Is(err, context.Canceled) {
			t.Errorf("Query() error = %v, want the process error on a live context, not context.Canceled", err)
		}
		if out != nil {
			t.Errorf("Query() = %q, want nil bytes on failure", out)
		}
	})
}

// tstQueryResult delivers the outcome of a Runner.Query started in the background.
type tstQueryResult struct {
	done chan struct{}
	out  []byte
	err  error
}

func tstQueryAsync(ctx context.Context, r *Runner, step plan.Step) *tstQueryResult {
	x := &tstQueryResult{done: make(chan struct{})}
	go func() {
		x.out, x.err = r.Query(ctx, step)
		close(x.done)
	}()
	return x
}

// Wait returns Query's result, failing t if it has not returned within timeout.
func (x *tstQueryResult) Wait(t *testing.T, timeout time.Duration) ([]byte, error) {
	t.Helper()
	select {
	case <-x.done:
		return x.out, x.err
	case <-time.After(timeout):
		t.Fatalf("Query did not return within %v", timeout)
		return nil, nil
	}
}

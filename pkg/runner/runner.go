package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// Runner executes plans against Engine, or prints their command lines when DryRun is set.
type Runner struct {
	Engine engine.Engine
	DryRun bool
	Strict bool
	Stdout io.Writer
	Stderr io.Writer

	run      func(ctx context.Context, argv []string, stdout, stderr io.Writer) error // nil runs argv through os/exec; argv[0] is Engine.Binary()
	interval time.Duration                                                            // WaitContainer poll interval; 0 means 1s
}

// rendered pairs a step with its full command line; probe is the inspect command a WaitContainer polls.
type rendered struct {
	step  plan.Step
	argv  []string
	probe []string
}

// Run renders every step and prints warnings before starting any process, then executes or prints the steps in order, stopping at the first error.
func (r *Runner) Run(ctx context.Context, p *plan.Plan, warnings []plan.Warning) error {
	var steps []plan.Step
	if p != nil {
		steps = p.Steps
	}
	all := append([]plan.Warning(nil), warnings...)
	cmds := make([]rendered, 0, len(steps))
	for _, step := range steps {
		argv, ws, err := r.Engine.Render(step)
		if err != nil {
			return err
		}
		all = append(all, ws...)
		c := rendered{step: step, argv: r.cmdline(argv)}
		if w, ok := step.(plan.WaitContainer); ok {
			probe, ws, err := r.Engine.Render(plan.InspectContainer{Name: w.Name})
			if err != nil {
				return err
			}
			all = append(all, ws...)
			c.probe = r.cmdline(probe)
		}
		cmds = append(cmds, c)
	}

	all = plan.NormalizeWarnings(all)
	for _, w := range all {
		fmt.Fprintf(r.Stderr, "warning: %s\n", w)
	}
	if r.Strict && len(all) > 0 {
		return fmt.Errorf("strict: refusing to continue with %d warning(s)", len(all))
	}

	if r.DryRun {
		for _, c := range cmds {
			if w, ok := c.step.(plan.WaitContainer); ok {
				fmt.Fprintf(r.Stdout, "# wait for %s: %s\n", w.Name, w.Condition)
			} else {
				fmt.Fprintln(r.Stdout, Quote(c.argv))
			}
		}
		return nil
	}

	for i := 0; i < len(cmds); {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		switch step := cmds[i].step.(type) {
		case plan.WaitContainer:
			err = r.wait(ctx, step, cmds[i].probe)
			i++
		case plan.Logs:
			j := i + 1
			for ; j < len(cmds); j++ {
				if _, ok := cmds[j].step.(plan.Logs); !ok {
					break
				}
			}
			err = r.logs(ctx, cmds[i:j])
			i = j
		default:
			err = r.execute(ctx, cmds[i].argv, r.Stdout, r.Stderr)
			i++
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Query runs a List* or InspectContainer step and returns its raw stdout for the Engine's Parse methods.
func (r *Runner) Query(ctx context.Context, step plan.Step) ([]byte, error) {
	switch step.(type) {
	case plan.ListContainers, plan.ListNetworks, plan.ListVolumes, plan.ListImages, plan.InspectContainer:
	default:
		return nil, fmt.Errorf("query: unsupported step %T", step)
	}
	if r.DryRun {
		return nil, errors.New("query: not available in dry run")
	}
	argv, warnings, err := r.Engine.Render(step)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		fmt.Fprintf(r.Stderr, "warning: %s\n", w)
	}
	out, err := r.capture(ctx, r.cmdline(argv))
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, err
}

func (r *Runner) cmdline(argv []string) []string {
	return append([]string{r.Engine.Binary()}, argv...)
}

// execute runs one command line, wrapping any failure with the quoted command.
func (r *Runner) execute(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	run := r.run
	if run == nil {
		run = execRun
	}
	if err := run(ctx, argv, stdout, stderr); err != nil {
		return fmt.Errorf("%s: %w", Quote(argv), err)
	}
	return nil
}

// capture runs argv with stdout collected and stderr passed through to Stderr.
func (r *Runner) capture(ctx context.Context, argv []string) ([]byte, error) {
	var out bytes.Buffer
	if err := r.execute(ctx, argv, &out, r.Stderr); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// execRun is the default run: on cancellation it sends SIGTERM, falling back to kill, and waits up to 5s for exit.
func execRun(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}

// wait polls probe until the container meets step.Condition or can no longer do so; only ctx bounds it.
func (r *Runner) wait(ctx context.Context, step plan.WaitContainer, probe []string) error {
	interval := r.interval
	if interval <= 0 {
		interval = time.Second
	}
	for {
		out, err := r.capture(ctx, probe)
		if err != nil {
			return fmt.Errorf("wait for %s (%s): %w", step.Name, step.Condition, err)
		}
		state, err := r.Engine.ParseInspect(out)
		if err != nil {
			return fmt.Errorf("wait for %s (%s): %w", step.Name, step.Condition, err)
		}
		met, failure := check(step.Condition, state)
		if failure != "" {
			return fmt.Errorf("wait for %s (%s): %s", step.Name, step.Condition, failure)
		}
		if met {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// check reports whether state meets cond, or the reason it never will.
func check(cond plan.Condition, s plan.ContainerState) (met bool, failure string) {
	switch cond {
	case plan.ConditionHealthy:
		switch {
		case s.Health == plan.HealthHealthy:
			return true, ""
		case s.Status == plan.StatusExited:
			return false, fmt.Sprintf("container exited with code %d", s.ExitCode)
		case s.Status == plan.StatusDead:
			return false, "container is dead"
		case s.Health == plan.HealthUnhealthy:
			return false, "container is unhealthy"
		case s.Status == plan.StatusRunning && s.Health == plan.HealthNone:
			return false, "container has no healthcheck"
		}
	case plan.ConditionCompleted:
		switch {
		case s.Status == plan.StatusExited && s.ExitCode == 0:
			return true, ""
		case s.Status == plan.StatusExited:
			return false, fmt.Sprintf("container exited with code %d", s.ExitCode)
		case s.Status == plan.StatusDead:
			return false, "container is dead"
		}
	}
	return false, ""
}

// logs runs a group of Logs steps concurrently, prefixing every line with the container name padded to the group's widest.
func (r *Runner) logs(ctx context.Context, cmds []rendered) error {
	if len(cmds) == 1 {
		return r.execute(ctx, cmds[0].argv, r.Stdout, r.Stderr)
	}
	width := 0
	for _, c := range cmds {
		width = max(width, len(c.step.(plan.Logs).Name))
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make([]error, len(cmds))
	for i, c := range cmds {
		name := c.step.(plan.Logs).Name
		prefix := name + strings.Repeat(" ", width-len(name)) + " | "
		stdout := &prefixWriter{w: r.Stdout, mu: &mu, prefix: prefix}
		stderr := &prefixWriter{w: r.Stderr, mu: &mu, prefix: prefix}
		wg.Go(func() {
			errs[i] = r.execute(ctx, c.argv, stdout, stderr)
			stdout.flush()
			stderr.flush()
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// prefixWriter emits whole prefixed lines under a mutex shared by the group, so lines of different processes never interleave.
type prefixWriter struct {
	w      io.Writer
	mu     *sync.Mutex
	prefix string
	buf    []byte
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(b)
	for {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			p.buf = append(p.buf, b...)
			return n, nil
		}
		p.buf = append(p.buf, b[:i]...)
		if err := p.line(); err != nil {
			return 0, err
		}
		b = b[i+1:]
	}
}

// flush emits the unterminated tail, if any, once the process has exited.
func (p *prefixWriter) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.buf) > 0 {
		p.line()
	}
}

// line writes prefix, the buffered line and a newline in a single call; the caller holds mu.
func (p *prefixWriter) line() error {
	out := make([]byte, 0, len(p.prefix)+len(p.buf)+1)
	out = append(append(append(out, p.prefix...), p.buf...), '\n')
	p.buf = p.buf[:0]
	_, err := p.w.Write(out)
	return err
}

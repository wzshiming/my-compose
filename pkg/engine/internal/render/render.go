package render

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/wzshiming/my-compose/pkg/plan"
)

// Builder accumulates one command's argv and warnings; the zero value is ready to use.
type Builder struct {
	scope string
	argv  []string
	ws    []plan.Warning
}

// SetScope sets the Scope of every warning added afterwards.
func (b *Builder) SetScope(scope string) {
	b.scope = scope
}

// Args appends args to the argv in order.
func (b *Builder) Args(args ...string) {
	b.argv = append(b.argv, args...)
}

// Repeat appends flag before each value.
func (b *Builder) Repeat(flag string, values []string) {
	for _, v := range values {
		b.argv = append(b.argv, flag, v)
	}
}

// KV appends flag before each pair as key=value, in the given order.
func (b *Builder) KV(flag string, kvs []plan.KV) {
	for _, kv := range kvs {
		b.argv = append(b.argv, flag, kv.Key+"="+kv.Value)
	}
}

// Ignored warns that attr is dropped.
func (b *Builder) Ignored(attr, reason string) {
	b.ws = append(b.ws, plan.Warning{Scope: b.scope, Attribute: attr, Message: "ignored (" + reason + ")"})
}

// IgnoredIf warns like Ignored when set.
func (b *Builder) IgnoredIf(set bool, attr, reason string) {
	if set {
		b.Ignored(attr, reason)
	}
}

// Degraded warns that attr is rendered as value instead.
func (b *Builder) Degraded(attr, value, reason string) {
	b.ws = append(b.ws, plan.Warning{Scope: b.scope, Attribute: attr, Message: "degraded to " + value + " (" + reason + ")"})
}

// Finish returns the argv as built and the normalized warnings.
func (b *Builder) Finish() ([]string, []plan.Warning) {
	return b.argv, plan.NormalizeWarnings(b.ws)
}

// Empty reports that a required field of step is unset.
func Empty(step plan.Step, field string) error {
	return fmt.Errorf("%T: empty %s", step, field)
}

// Unsupported reports a step the dialect does not render.
func Unsupported(step plan.Step) error {
	return fmt.Errorf("unsupported step %T", step)
}

// Seconds rounds d up to whole seconds.
func Seconds(d time.Duration) string {
	return strconv.FormatInt(int64(math.Ceil(d.Seconds())), 10)
}

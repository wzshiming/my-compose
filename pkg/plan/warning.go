package plan

import (
	"cmp"
	"slices"
)

// Warning fields: Scope is a compose path root like "services.web", Attribute is relative to it, and Message is "ignored (<reason>)" or "degraded to <value> (<reason>)", lowercase with no period.
type Warning struct {
	Scope     string
	Attribute string
	Message   string
}

func (w Warning) String() string {
	return w.Scope + ": " + w.Attribute + ": " + w.Message
}

func NormalizeWarnings(ws []Warning) []Warning {
	out := append([]Warning{}, ws...)
	slices.SortFunc(out, func(a, b Warning) int {
		return cmp.Or(
			cmp.Compare(a.Scope, b.Scope),
			cmp.Compare(a.Attribute, b.Attribute),
			cmp.Compare(a.Message, b.Message),
		)
	})
	return slices.Compact(out)
}

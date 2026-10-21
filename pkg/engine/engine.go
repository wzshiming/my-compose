package engine

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wzshiming/my-compose/pkg/plan"
)

type Engine interface {
	// Name is the selection key.
	Name() string
	// Binary is the command name to execute, never a path.
	Binary() string
	Capabilities() plan.Capabilities
	// Render is pure: argv[0] is the subcommand, without Binary or shell quoting; WaitContainer yields an empty argv for the runner to interpret; nil or unknown steps, and a healthy wait without Capabilities().Healthcheck, are errors; inexpressible attributes become sorted, deduplicated warnings.
	Render(step plan.Step) (argv []string, warnings []plan.Warning, err error)
	// Parse methods take the raw stdout of the matching List* or InspectContainer command; blank input yields an empty slice and nil error (an error for ParseInspect), invalid JSON an error.
	ParseContainers(out []byte) ([]plan.ContainerState, error)
	ParseNetworks(out []byte) ([]string, error)
	ParseVolumes(out []byte) ([]string, error)
	ParseImages(out []byte) ([]string, error)
	ParseInspect(out []byte) (plan.ContainerState, error)
}

var (
	ErrNoEngine      = errors.New("no container engine found")
	ErrUnknownEngine = errors.New("unknown engine")
)

// Order lists engine names in the order Resolve probes them on goos.
func Order(goos string) []string {
	switch goos {
	case "darwin":
		return []string{"container", "docker", "podman", "nerdctl"}
	case "windows":
		return []string{"wslc", "docker", "podman", "nerdctl"}
	default:
		return []string{"docker", "podman", "nerdctl"}
	}
}

// Resolve returns the candidate named explicit, or else the first candidate in Order(goos) whose Binary lookPath finds.
func Resolve(explicit, goos string, lookPath func(string) (string, error), candidates []Engine) (Engine, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		var names []string
		for _, e := range candidates {
			if e.Name() == explicit {
				return e, nil
			}
			names = append(names, e.Name())
		}
		return nil, fmt.Errorf("unknown engine %q: available engines are %s: %w", explicit, strings.Join(names, ", "), ErrUnknownEngine)
	}
	var tried []string
	for _, name := range Order(goos) {
		i := slices.IndexFunc(candidates, func(e Engine) bool { return e.Name() == name })
		if i < 0 {
			continue
		}
		tried = append(tried, name)
		if _, err := lookPath(candidates[i].Binary()); err == nil {
			return candidates[i], nil
		}
	}
	return nil, fmt.Errorf("no container engine found in PATH (tried: %s); install one or select with --engine or CONTAINER_COMPOSE_ENGINE: %w", strings.Join(tried, ", "), ErrNoEngine)
}

package plan

import (
	"fmt"
	"maps"
	"slices"

	"github.com/compose-spec/compose-go/v2/types"
)

type UpOptions struct {
	Build    bool
	Services []string
}

func Up(project *types.Project, state State, caps Capabilities, opts UpOptions) (*Plan, []Warning, error) {
	sorted, err := topoOrder(project)
	if err != nil {
		return nil, nil, err
	}
	in, err := upScope(project, opts.Services)
	if err != nil {
		return nil, nil, err
	}
	var services []string
	names := map[string][]string{}
	for _, key := range sorted {
		if !in[key] {
			continue
		}
		if names[key], err = replicaNames(project, key); err != nil {
			return nil, nil, err
		}
		services = append(services, key)
	}

	plan := &Plan{}
	for _, key := range usedNetworks(project, services) {
		if spec := toNetworkSpec(project, key); !slices.Contains(state.Networks, spec.Name) {
			plan.Steps = append(plan.Steps, CreateNetwork{Network: spec})
		}
	}
	for _, key := range usedVolumes(project, services) {
		if spec := toVolumeSpec(project, key); !slices.Contains(state.Volumes, spec.Name) {
			plan.Steps = append(plan.Steps, CreateVolume{Volume: spec})
		}
	}

	images := map[string]bool{}
	for _, img := range state.Images {
		images[normalizeImage(img)] = true
	}
	existing := map[string]ContainerState{}
	for _, c := range state.Containers {
		existing[c.Name] = c
	}
	built := map[string]bool{}
	waited := map[WaitContainer]bool{}
	ws := ignoredProjectAttrs(project)
	for _, key := range services {
		s := project.Services[key]
		ws = append(ws, ignoredServiceAttrs(key, s)...)

		policy, _, err := s.GetPullPolicy()
		if err != nil {
			return nil, nil, fmt.Errorf("service %q: %w", key, err)
		}
		forceBuild := policy == types.PullPolicyBuild
		switch policy {
		case types.PullPolicyAlways, types.PullPolicyNever:
		case types.PullPolicyRefresh:
			policy = types.PullPolicyAlways
			ws = append(ws, Warning{Scope: "services." + key, Attribute: "pull_policy", Message: "degraded to always (scheduled refresh is not supported)"})
		default:
			policy = types.PullPolicyMissing
		}

		image := s.Image
		if image == "" && s.Build != nil {
			image = ImageName(project.Name, key)
		}
		if s.Build != nil && !built[image] && (opts.Build || forceBuild || !images[normalizeImage(image)]) {
			built[image] = true
			plan.Steps = append(plan.Steps, BuildImage{Build: toBuildSpec(project, key, s.Build, image)})
		}

		var stale []ContainerState
		for _, c := range state.Containers {
			if c.Service == key && !slices.Contains(names[key], c.Name) {
				stale = append(stale, c)
			}
		}
		var start, run []Step
		for i, name := range names[key] {
			c, ok := existing[name]
			switch {
			case !ok:
			case c.Status == StatusRunning, c.Status == StatusRestarting, c.Status == StatusPaused:
				continue
			case c.Status == StatusDead:
				run = append(run, RemoveContainers{Names: []string{name}})
			default: // one step per container: some engines start a single container at a time
				start = append(start, StartContainers{Names: []string{name}})
				continue
			}
			run = append(run, RunContainer{Container: toContainerSpec(project, key, s, i+1, image, policy)})
		}

		for _, dep := range slices.Sorted(maps.Keys(s.DependsOn)) {
			target, ok := project.Services[dep]
			if !ok {
				continue
			}
			var cond Condition
			switch s.DependsOn[dep].Condition {
			case types.ServiceConditionCompletedSuccessfully:
				cond = ConditionCompleted
			case types.ServiceConditionHealthy:
				hc := target.HealthCheck
				if !caps.Healthcheck || hc != nil && (hc.Disable || len(hc.Test) > 0 && hc.Test[0] == "NONE") {
					reason := "healthcheck disabled"
					if !caps.Healthcheck {
						reason = "engine has no healthcheck support"
					}
					ws = append(ws, Warning{Scope: "services." + key, Attribute: "depends_on." + dep + ".condition", Message: "degraded to service_started (" + reason + ")"})
					continue
				}
				cond = ConditionHealthy
			default:
				continue
			}
			if len(start) == 0 && len(run) == 0 {
				continue
			}
			for _, name := range names[dep] {
				if w := (WaitContainer{Name: name, Condition: cond}); !waited[w] {
					waited[w] = true
					plan.Steps = append(plan.Steps, w)
				}
			}
		}

		if len(stale) > 0 {
			slices.SortFunc(stale, compareByNumber)
			var stop, gone []string
			for _, c := range stale {
				gone = append(gone, c.Name)
				if c.Status != StatusExited && c.Status != StatusCreated && c.Status != StatusDead {
					stop = append(stop, c.Name)
				}
			}
			if len(stop) > 0 {
				plan.Steps = append(plan.Steps, StopContainers{Names: stop})
			}
			plan.Steps = append(plan.Steps, RemoveContainers{Names: gone})
		}
		plan.Steps = append(plan.Steps, start...)
		plan.Steps = append(plan.Steps, run...)
	}
	return plan, NormalizeWarnings(ws), nil
}

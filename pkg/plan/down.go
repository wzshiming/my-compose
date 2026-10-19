package plan

import (
	"maps"
	"slices"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
)

type DownOptions struct {
	Volumes bool
	Timeout *time.Duration
}

func Down(project *types.Project, state State, opts DownOptions) (*Plan, []Warning, error) {
	services, err := topoOrder(project)
	if err != nil {
		return nil, nil, err
	}
	slices.Reverse(services)

	groups := map[string][]ContainerState{}
	if state.Live {
		for _, c := range state.Containers {
			groups[c.Service] = append(groups[c.Service], c)
		}
	} else {
		for _, key := range services {
			names, err := replicaNames(project, key)
			if err != nil {
				return nil, nil, err
			}
			for i, name := range names {
				groups[key] = append(groups[key], ContainerState{Name: name, Service: key, Number: i + 1})
			}
		}
	}

	plan := &Plan{}
	remove := func(cs []ContainerState, timeout *time.Duration) {
		if len(cs) == 0 {
			return
		}
		slices.SortFunc(cs, compareByNumber)
		var stop, all []string
		for _, c := range cs {
			all = append(all, c.Name)
			if c.Status != StatusExited && c.Status != StatusCreated && c.Status != StatusDead {
				stop = append(stop, c.Name)
			}
		}
		if len(stop) > 0 {
			plan.Steps = append(plan.Steps, StopContainers{Names: stop, Timeout: timeout})
		}
		plan.Steps = append(plan.Steps, RemoveContainers{Names: all, Volumes: opts.Volumes})
	}
	for _, key := range services {
		timeout := opts.Timeout
		if timeout == nil {
			timeout = stopTimeout(project.Services[key].StopGracePeriod)
		}
		remove(groups[key], timeout)
		delete(groups, key)
	}
	for _, key := range slices.Sorted(maps.Keys(groups)) {
		remove(groups[key], opts.Timeout)
	}

	var networks, volumes []string
	if state.Live {
		externalNets, externalVols := map[string]bool{}, map[string]bool{}
		for key, n := range project.Networks {
			if n.External {
				externalNets[NetworkName(project.Name, key, n)] = true
			}
		}
		for key, v := range project.Volumes {
			if v.External {
				externalVols[VolumeName(project.Name, key, v)] = true
			}
		}
		networks = slices.DeleteFunc(slices.Sorted(slices.Values(state.Networks)), func(n string) bool { return externalNets[n] })
		volumes = slices.DeleteFunc(slices.Sorted(slices.Values(state.Volumes)), func(v string) bool { return externalVols[v] })
	} else {
		for _, key := range usedNetworks(project, services) {
			networks = append(networks, NetworkName(project.Name, key, project.Networks[key]))
		}
		for _, key := range usedVolumes(project, services) {
			volumes = append(volumes, VolumeName(project.Name, key, project.Volumes[key]))
		}
	}
	for _, n := range networks {
		plan.Steps = append(plan.Steps, RemoveNetwork{Name: n})
	}
	if opts.Volumes {
		for _, v := range volumes {
			plan.Steps = append(plan.Steps, RemoveVolume{Name: v})
		}
	}
	return plan, NormalizeWarnings(nil), nil
}

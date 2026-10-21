package plan

import (
	"cmp"
	"slices"

	"github.com/compose-spec/compose-go/v2/types"
)

type LogsOptions struct {
	Services []string
	Follow   bool
	Tail     string
}

func ServiceLogs(project *types.Project, state State, opts LogsOptions) (*Plan, []Warning, error) {
	if err := checkServices(project, opts.Services); err != nil {
		return nil, nil, err
	}
	selected := func(svc string) bool {
		_, ok := project.Services[svc]
		return ok && (len(opts.Services) == 0 || slices.Contains(opts.Services, svc))
	}
	var cs []ContainerState
	if state.Live {
		for _, c := range state.Containers {
			if selected(c.Service) {
				cs = append(cs, c)
			}
		}
	} else {
		for _, key := range project.ServiceNames() {
			if !selected(key) {
				continue
			}
			names, err := replicaNames(project, key)
			if err != nil {
				return nil, nil, err
			}
			for i, name := range names {
				cs = append(cs, ContainerState{Name: name, Service: key, Number: i + 1})
			}
		}
	}
	slices.SortFunc(cs, func(a, b ContainerState) int {
		return cmp.Or(cmp.Compare(a.Service, b.Service), compareByNumber(a, b))
	})
	plan := &Plan{}
	for _, c := range cs {
		plan.Steps = append(plan.Steps, Logs{Name: c.Name, Follow: opts.Follow, Tail: opts.Tail})
	}
	return plan, NormalizeWarnings(nil), nil
}

func Ps(project *types.Project) (*Plan, []Warning, error) {
	return &Plan{Steps: []Step{ListContainers{Project: project.Name}}}, NormalizeWarnings(nil), nil
}

func StateQueries(project *types.Project) (*Plan, []Warning, error) {
	return &Plan{Steps: []Step{
		ListContainers{Project: project.Name},
		ListNetworks{},
		ListVolumes{},
		ListImages{},
	}}, NormalizeWarnings(nil), nil
}

package plan

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
)

func topoOrder(p *types.Project) ([]string, error) {
	blocked := map[string]int{}
	dependents := map[string][]string{}
	for _, key := range p.ServiceNames() {
		deps := p.Services[key].DependsOn
		for _, dep := range slices.Sorted(maps.Keys(deps)) {
			if _, ok := p.Services[dep]; ok {
				blocked[key]++
				dependents[dep] = append(dependents[dep], key)
			} else if deps[dep].Required {
				return nil, fmt.Errorf("service %q depends on undefined service %q", key, dep)
			}
		}
	}
	var ready, sorted []string
	for key := range p.Services {
		if blocked[key] == 0 {
			ready = append(ready, key)
		}
	}
	for len(ready) > 0 {
		slices.Sort(ready)
		key := ready[0]
		ready = ready[1:]
		sorted = append(sorted, key)
		for _, d := range dependents[key] {
			if blocked[d]--; blocked[d] == 0 {
				ready = append(ready, d)
			}
		}
	}
	if len(sorted) < len(p.Services) {
		var cycle []string
		for key, n := range blocked {
			if n > 0 {
				cycle = append(cycle, key)
			}
		}
		slices.Sort(cycle)
		return nil, fmt.Errorf("dependency cycle among services %s", strings.Join(cycle, ", "))
	}
	return sorted, nil
}

func checkServices(p *types.Project, names []string) error {
	for _, name := range names {
		if _, ok := p.Services[name]; !ok {
			return fmt.Errorf("no such service: %s", name)
		}
	}
	return nil
}

func upScope(p *types.Project, names []string) (map[string]bool, error) {
	if err := checkServices(p, names); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		names = p.ServiceNames()
	}
	in := map[string]bool{}
	var add func(string)
	add = func(key string) {
		if _, ok := p.Services[key]; !ok || in[key] {
			return
		}
		in[key] = true
		for dep := range p.Services[key].DependsOn {
			add(dep)
		}
	}
	for _, name := range names {
		add(name)
	}
	return in, nil
}

func compareByNumber(a, b ContainerState) int {
	return cmp.Or(cmp.Compare(a.Number, b.Number), cmp.Compare(a.Name, b.Name))
}

func usedNetworks(p *types.Project, services []string) []string {
	used := map[string]bool{}
	for _, svc := range services {
		for key := range p.Services[svc].Networks {
			if !p.Networks[key].External {
				used[key] = true
			}
		}
	}
	return sortByEngineName(used, func(key string) string { return NetworkName(p.Name, key, p.Networks[key]) })
}

func usedVolumes(p *types.Project, services []string) []string {
	used := map[string]bool{}
	for _, svc := range services {
		for _, v := range p.Services[svc].Volumes {
			if cfg, ok := p.Volumes[v.Source]; ok && v.Type == types.VolumeTypeVolume && !bool(cfg.External) {
				used[v.Source] = true
			}
		}
	}
	return sortByEngineName(used, func(key string) string { return VolumeName(p.Name, key, p.Volumes[key]) })
}

func sortByEngineName(keys map[string]bool, name func(string) string) []string {
	sorted := slices.Collect(maps.Keys(keys))
	slices.SortFunc(sorted, func(a, b string) int {
		return cmp.Or(cmp.Compare(name(a), name(b)), cmp.Compare(a, b))
	})
	return sorted
}

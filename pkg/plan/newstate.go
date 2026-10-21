package plan

import (
	"slices"
	"strconv"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
)

func NewState(project *types.Project, containers []ContainerState, networks, volumes, images []string) State {
	fixed := map[string]string{}
	for key, s := range project.Services {
		if s.ContainerName != "" {
			fixed[s.ContainerName] = key
		}
	}
	st := State{Live: true}
	for _, c := range containers {
		switch {
		case len(c.Labels) > 0:
			if c.Labels[LabelProject] != project.Name {
				continue
			}
			n, err := strconv.Atoi(c.Labels[LabelContainerNumber])
			if err != nil {
				n = 0
			}
			c.Service, c.Number = c.Labels[LabelService], n
		case fixed[c.Name] != "":
			c.Service, c.Number = fixed[c.Name], 1
		default:
			rest, ok := strings.CutPrefix(c.Name, project.Name+"-")
			i := strings.LastIndex(rest, "-")
			if !ok || i < 0 {
				continue
			}
			n, err := strconv.Atoi(rest[i+1:])
			if _, known := project.Services[rest[:i]]; !known || err != nil {
				continue
			}
			c.Service, c.Number = rest[:i], n
		}
		st.Containers = append(st.Containers, c)
	}

	declared := map[string]bool{}
	for key, n := range project.Networks {
		declared[NetworkName(project.Name, key, n)] = true
	}
	st.Networks = keepOwned(project.Name, networks, declared)
	declared = map[string]bool{}
	for key, v := range project.Volumes {
		declared[VolumeName(project.Name, key, v)] = true
	}
	st.Volumes = keepOwned(project.Name, volumes, declared)
	for _, img := range images {
		st.Images = append(st.Images, normalizeImage(img))
	}
	return st
}

func keepOwned(project string, names []string, declared map[string]bool) []string {
	var kept []string
	for _, n := range names {
		if declared[n] || strings.HasPrefix(n, project+"_") {
			kept = append(kept, n)
		}
	}
	slices.Sort(kept)
	return slices.Compact(kept)
}

// normalizeImage makes equivalent references equal, e.g. nginx and docker.io/library/nginx:latest.
func normalizeImage(s string) string {
	// podman/buildah store unqualified local builds as localhost/<name>, so it names the same image.
	s = strings.TrimPrefix(s, "localhost/")
	if r, err := reference.ParseDockerRef(s); err == nil {
		return r.String()
	}
	return s
}

package plan

import (
	"cmp"
	"reflect"
	"slices"
	"testing"
)

const stateYAML = `
services:
  web:
    image: nginx
  db:
    image: postgres
    container_name: mydb
networks:
  shared:
    name: shared-net
volumes:
  keep:
    name: kept-vol
`

type testContainerID struct {
	Name, Service string
	Number        int
}

func containerIDs(s State) []testContainerID {
	out := []testContainerID{}
	for _, c := range s.Containers {
		out = append(out, testContainerID{c.Name, c.Service, c.Number})
	}
	slices.SortFunc(out, func(a, b testContainerID) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func TestAC10NewState(t *testing.T) {
	tests := []struct {
		name string
		in   []ContainerState
		want []testContainerID
	}{
		{"containers of other projects are dropped",
			[]ContainerState{{Name: "p-web-1", Labels: composeLabelsFor("p", "web", "1")}, {Name: "q-web-1", Labels: composeLabelsFor("q", "web", "1")}},
			[]testContainerID{{"p-web-1", "web", 1}}},
		{"labelled containers of services missing from the file are kept",
			[]ContainerState{{Name: "p-old-3", Labels: composeLabelsFor("p", "old", "3")}},
			[]testContainerID{{"p-old-3", "old", 3}}},
		{"unparsable number label becomes 0",
			[]ContainerState{{Name: "custom", Labels: composeLabelsFor("p", "web", "x")}},
			[]testContainerID{{"custom", "web", 0}}},
		{"unlabelled replica name is adopted",
			[]ContainerState{{Name: "p-web-2"}},
			[]testContainerID{{"p-web-2", "web", 2}}},
		{"unlabelled container_name is adopted as replica 1",
			[]ContainerState{{Name: "mydb"}},
			[]testContainerID{{"mydb", "db", 1}}},
		{"unlabelled names of no file service are dropped",
			[]ContainerState{{Name: "p-ghost-1"}, {Name: "other"}, {Name: "p-web-1"}},
			[]testContainerID{{"p-web-1", "web", 1}}},
		{"labels without the project label are dropped",
			[]ContainerState{{Name: "p-web-3", Labels: map[string]string{"app": "demo"}}, {Name: "p-web-1", Labels: composeLabelsFor("p", "web", "1")}},
			[]testContainerID{{"p-web-1", "web", 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerIDs(NewState(loadProject(t, stateYAML), tt.in, nil, nil, nil)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Containers = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("declared and project networks kept, sorted and deduplicated", func(t *testing.T) {
		in := []string{"bridge", "p_default", "shared-net", "p_old", "host", "p_default"}
		got := NewState(loadProject(t, stateYAML), nil, in, nil, nil).Networks
		if want := []string{"p_default", "p_old", "shared-net"}; !slices.Equal(got, want) {
			t.Errorf("Networks = %v, want %v", got, want)
		}
	})
	t.Run("declared and project volumes kept, sorted and deduplicated", func(t *testing.T) {
		in := []string{"p_data", "other", "kept-vol", "p_data"}
		got := NewState(loadProject(t, stateYAML), nil, nil, in, nil).Volumes
		if want := []string{"kept-vol", "p_data"}; !slices.Equal(got, want) {
			t.Errorf("Volumes = %v, want %v", got, want)
		}
	})
	t.Run("images normalized, unparsable ones kept", func(t *testing.T) {
		got := NewState(loadProject(t, stateYAML), nil, nil, nil, []string{"nginx", "redis:7", "UPPER"}).Images
		if want := []string{"UPPER", "docker.io/library/nginx:latest", "docker.io/library/redis:7"}; !slices.Equal(sortedCopy(got), want) {
			t.Errorf("Images = %v, want %v in any order", got, want)
		}
	})
	t.Run("localhost/ image of a local build is folded into the bare name", func(t *testing.T) {
		got := NewState(loadProject(t, stateYAML), nil, nil, nil, []string{"localhost/e2e-app:latest"}).Images
		if want := []string{"docker.io/library/e2e-app:latest"}; !slices.Equal(got, want) {
			t.Errorf("Images = %v, want %v", got, want)
		}
	})
	t.Run("normalizeImage equates localhost/ with the bare name", func(t *testing.T) {
		tests := []struct{ in, want string }{
			{"localhost/e2e-app:latest", "docker.io/library/e2e-app:latest"},
			{"localhost/e2e-app", "docker.io/library/e2e-app:latest"},
			{"e2e-app", "docker.io/library/e2e-app:latest"},
			{"e2e-app:latest", "docker.io/library/e2e-app:latest"},
			{"localhost/ns/foo:1", "docker.io/ns/foo:1"},
			{"localhost:5000/foo:1", "localhost:5000/foo:1"},
			{"nginx", "docker.io/library/nginx:latest"},
			{"e2e-app:", "e2e-app:"},
			{"", ""},
		}
		for _, tt := range tests {
			if got := normalizeImage(tt.in); got != tt.want {
				t.Errorf("normalizeImage(%q) = %q, want %q", tt.in, got, tt.want)
			}
		}
	})
}

func TestG1NewStateIsLive(t *testing.T) {
	if s := NewState(loadProject(t, stateYAML), nil, nil, nil, nil); !s.Live {
		t.Error("NewState(...).Live = false, want true even with nothing found")
	}
}

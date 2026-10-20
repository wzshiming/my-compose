package container

import (
	"cmp"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/wzshiming/my-compose/pkg/engine/internal/parse"
	"github.com/wzshiming/my-compose/pkg/plan"
)

type containerJSON struct {
	ID            string `json:"id"`
	Configuration struct {
		ID    string `json:"id"`
		Image struct {
			Reference string `json:"reference"`
		} `json:"image"`
		Labels         map[string]string `json:"labels"`
		PublishedPorts []struct {
			HostAddress   string `json:"hostAddress"`
			HostPort      int    `json:"hostPort"`
			ContainerPort int    `json:"containerPort"`
			Proto         string `json:"proto"`
			Count         int    `json:"count"`
		} `json:"publishedPorts"`
		InitProcess struct {
			Executable string   `json:"executable"`
			Arguments  []string `json:"arguments"`
		} `json:"initProcess"`
	} `json:"configuration"`
	Status json.RawMessage `json:"status"` // an object with a state field or a bare string
}

type namedJSON struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Configuration struct {
		Name string `json:"name"`
	} `json:"configuration"`
}

func (dialect) ParseContainers(out []byte) ([]plan.ContainerState, error) {
	items, err := parse.List[containerJSON](out)
	if err != nil {
		return nil, err
	}
	states := make([]plan.ContainerState, 0, len(items))
	for _, c := range items {
		states = append(states, c.state())
	}
	return states, nil
}

func (dialect) ParseInspect(out []byte) (plan.ContainerState, error) {
	items, err := parse.List[containerJSON](out)
	if err != nil {
		return plan.ContainerState{}, err
	}
	if len(items) == 0 {
		return plan.ContainerState{}, errors.New("inspect output lists no container")
	}
	return items[0].state(), nil
}

func (dialect) ParseNetworks(out []byte) ([]string, error) {
	return names(out, func(n namedJSON) string { return cmp.Or(n.ID, n.Configuration.Name) })
}

func (dialect) ParseVolumes(out []byte) ([]string, error) {
	return names(out, func(n namedJSON) string { return cmp.Or(n.ID, n.Configuration.Name, n.Name) })
}

func (dialect) ParseImages(out []byte) ([]string, error) {
	return names(out, func(n namedJSON) string { return n.Configuration.Name })
}

func names(out []byte, pick func(namedJSON) string) ([]string, error) {
	items, err := parse.List[namedJSON](out)
	if err != nil {
		return nil, err
	}
	return parse.Names(items, pick), nil
}

func (c containerJSON) state() plan.ContainerState {
	cfg := c.Configuration
	labels := cfg.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	number, _ := strconv.Atoi(labels[plan.LabelContainerNumber])
	s := plan.ContainerState{
		Name:    cmp.Or(cfg.ID, c.ID),
		Service: labels[plan.LabelService],
		Number:  number,
		Image:   cfg.Image.Reference,
		Status:  status(c.Status),
		Health:  plan.HealthNone,
		Labels:  labels,
	}
	if s.Status == plan.StatusExited {
		s.ExitCode = -1
	}
	for _, p := range cfg.PublishedPorts {
		for i := range max(p.Count, 1) {
			s.Ports = append(s.Ports, plan.Port{
				HostIP:    p.HostAddress,
				Published: strconv.Itoa(p.HostPort + i),
				Target:    uint32(p.ContainerPort + i),
				Protocol:  p.Proto,
			})
		}
	}
	if cfg.InitProcess.Executable != "" {
		s.Command = append([]string{cfg.InitProcess.Executable}, cfg.InitProcess.Arguments...)
	}
	return s
}

func status(raw json.RawMessage) string {
	var state string
	if err := json.Unmarshal(raw, &state); err != nil {
		var obj struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal(raw, &obj)
		state = obj.State
	}
	switch strings.ToLower(state) {
	case "running", "stopping":
		return plan.StatusRunning
	case "stopped":
		return plan.StatusExited
	}
	return plan.StatusUnknown
}

package dockercli

import (
	"cmp"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/wzshiming/my-compose/pkg/engine/internal/parse"
	"github.com/wzshiming/my-compose/pkg/plan"
)

// psRow is the union of the ps fields the CLIs emit; RawMessage fields differ in JSON type between them.
type psRow struct {
	Names, Labels, Ports, Command json.RawMessage
	Image, State, Status          string
	HealthStatus                  string
	ExitCode                      *int
}

func (dialect) ParseContainers(out []byte) ([]plan.ContainerState, error) {
	rows, err := parse.List[psRow](out)
	if err != nil {
		return nil, err
	}
	states := make([]plan.ContainerState, 0, len(rows))
	for _, r := range rows {
		s := plan.ContainerState{
			Name:    parseName(r.Names),
			Image:   r.Image,
			Status:  containerStatus(r.State, r.Status),
			Health:  containerHealth(r.HealthStatus, r.Status),
			Ports:   parsePorts(r.Ports),
			Command: parseCommand(r.Command),
		}
		s.ExitCode = containerExitCode(r.ExitCode, r.Status, s.Status)
		states = append(states, fromLabels(s, parseLabels(r.Labels)))
	}
	return states, nil
}

type inspectRow struct {
	Name      string
	ImageName string
	Config    struct {
		Image  string
		Labels map[string]string
		Cmd    []string
	}
	State struct {
		Status      string
		ExitCode    int
		Health      struct{ Status string }
		Healthcheck struct{ Status string }
	}
}

func (dialect) ParseInspect(out []byte) (plan.ContainerState, error) {
	rows, err := parse.List[inspectRow](out)
	if err != nil {
		return plan.ContainerState{}, err
	}
	if len(rows) == 0 {
		return plan.ContainerState{}, errors.New("inspect: no container in output")
	}
	r := rows[0]
	return fromLabels(plan.ContainerState{
		Name:     strings.TrimPrefix(r.Name, "/"),
		Image:    cmp.Or(r.Config.Image, r.ImageName),
		Status:   containerStatus(r.State.Status, ""),
		Health:   cmp.Or(r.State.Health.Status, r.State.Healthcheck.Status, plan.HealthNone),
		ExitCode: r.State.ExitCode,
		Command:  r.Config.Cmd,
	}, r.Config.Labels), nil
}

func (dialect) ParseNetworks(out []byte) ([]string, error) { return names(out) }

func (dialect) ParseVolumes(out []byte) ([]string, error) { return names(out) }

func (dialect) ParseImages(out []byte) ([]string, error) {
	rows, err := parse.List[struct {
		Names           []string
		Repository, Tag string
	}](out)
	if err != nil {
		return nil, err
	}
	images := make([]string, 0, len(rows))
	for _, r := range rows {
		switch {
		case len(r.Names) > 0:
			images = append(images, r.Names...)
		case r.Repository != "" && r.Repository != "<none>" && r.Tag != "" && r.Tag != "<none>":
			images = append(images, r.Repository+":"+r.Tag)
		}
	}
	return images, nil
}

func names(out []byte) ([]string, error) {
	rows, err := parse.List[struct{ Name string }](out)
	if err != nil {
		return nil, err
	}
	return parse.Names(rows, func(r struct{ Name string }) string { return r.Name }), nil
}

// fromLabels stores labels on s and fills the fields the compose labels carry.
func fromLabels(s plan.ContainerState, labels map[string]string) plan.ContainerState {
	if labels == nil {
		labels = map[string]string{}
	}
	s.Labels = labels
	s.Service = labels[plan.LabelService]
	s.Number, _ = strconv.Atoi(labels[plan.LabelContainerNumber])
	return s
}

// containerStatus maps a State value, or without one the first word of Status, onto the plan statuses.
func containerStatus(state, status string) string {
	if state == "" {
		state, _, _ = strings.Cut(status, " ")
	}
	switch s := strings.ToLower(state); s {
	case plan.StatusCreated, plan.StatusRunning, plan.StatusPaused, plan.StatusRestarting, plan.StatusExited, plan.StatusDead:
		return s
	case "up":
		if strings.Contains(status, "(Paused)") {
			return plan.StatusPaused
		}
		return plan.StatusRunning
	case "stopped":
		return plan.StatusExited
	case "configured", "initialized":
		return plan.StatusCreated
	}
	return plan.StatusUnknown
}

func containerHealth(health, status string) string {
	switch {
	case health != "":
		return health
	case strings.HasSuffix(status, "(healthy)"):
		return plan.HealthHealthy
	case strings.HasSuffix(status, "(unhealthy)"):
		return plan.HealthUnhealthy
	case strings.HasSuffix(status, "(health: starting)"):
		return plan.HealthStarting
	}
	return plan.HealthNone
}

func containerExitCode(code *int, status, mapped string) int {
	if code != nil {
		return *code
	}
	if _, rest, ok := strings.Cut(status, "Exited ("); ok {
		if text, _, ok := strings.Cut(rest, ")"); ok {
			if n, err := strconv.Atoi(text); err == nil {
				return n
			}
		}
	}
	if mapped == plan.StatusExited || mapped == plan.StatusDead {
		return -1
	}
	return 0
}

// parseName takes the first of a string or array of names, dropping a leading slash and extra comma-joined names.
func parseName(raw json.RawMessage) string {
	names := parseStrings(raw)
	if len(names) == 0 {
		return ""
	}
	name, _, _ := strings.Cut(names[0], ",")
	return strings.TrimPrefix(name, "/")
}

// parseStrings decodes a JSON string or array of strings.
func parseStrings(raw json.RawMessage) []string {
	var out []string
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		json.Unmarshal(raw, &s)
		return []string{s}
	}
	json.Unmarshal(raw, &out)
	return out
}

// parseLabels reads a label map or a "k=v,k=v" string, folding comma-only segments back into the preceding value.
func parseLabels(raw json.RawMessage) map[string]string {
	labels := map[string]string{}
	if len(raw) > 0 && raw[0] == '{' {
		json.Unmarshal(raw, &labels)
		return labels
	}
	var text, last string
	json.Unmarshal(raw, &text)
	for _, seg := range strings.Split(text, ",") {
		if k, v, ok := strings.Cut(seg, "="); ok {
			labels[k] = v
			last = k
		} else if last != "" {
			labels[last] += "," + seg
		}
	}
	return labels
}

// parseCommand keeps an array as is and splits a string after stripping the outer quotes some CLIs add.
func parseCommand(raw json.RawMessage) []string {
	if len(raw) > 0 && raw[0] == '[' {
		return parseStrings(raw)
	}
	var text string
	json.Unmarshal(raw, &text)
	if fields := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(text, `"`), `"`)); len(fields) > 0 {
		return fields
	}
	return nil
}

// parsePorts reads a list of port objects or an "ip:host->target/proto, ..." string.
func parsePorts(raw json.RawMessage) []plan.Port {
	var ports []plan.Port
	if len(raw) > 0 && raw[0] == '[' {
		var rows []struct {
			HostIP        string `json:"host_ip"`
			HostPort      uint32 `json:"host_port"`
			ContainerPort uint32 `json:"container_port"`
			Protocol      string `json:"protocol"`
		}
		json.Unmarshal(raw, &rows)
		for _, p := range rows {
			port := plan.Port{HostIP: p.HostIP, Target: p.ContainerPort, Protocol: p.Protocol}
			if p.HostPort != 0 {
				port.Published = strconv.FormatUint(uint64(p.HostPort), 10)
			}
			ports = append(ports, port)
		}
		return ports
	}
	var text string
	json.Unmarshal(raw, &text)
	for _, seg := range strings.Split(text, ",") {
		if p, ok := parsePort(strings.TrimSpace(seg)); ok {
			ports = append(ports, p)
		}
	}
	return ports
}

// parsePort reads "[ip:]host->target/proto" or "target/proto", where host and target may be ranges.
func parsePort(seg string) (plan.Port, bool) {
	var p plan.Port
	host, target, mapped := strings.Cut(seg, "->")
	if !mapped {
		host, target = "", host
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		p.HostIP, p.Published = strings.Trim(host[:i], "[]"), host[i+1:]
	} else {
		p.Published = host
	}
	portText, proto, _ := strings.Cut(target, "/")
	first, _, _ := strings.Cut(portText, "-")
	n, err := strconv.ParseUint(first, 10, 32)
	if err != nil {
		return plan.Port{}, false
	}
	p.Target, p.Protocol = uint32(n), proto
	return p, true
}

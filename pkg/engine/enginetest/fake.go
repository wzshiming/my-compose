package enginetest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/wzshiming/my-compose/pkg/engine"
	"github.com/wzshiming/my-compose/pkg/plan"
)

var _ engine.Engine = (*Fake)(nil)

// Fake is a scripted engine.Engine that records its inputs; it is not safe for concurrent use.
type Fake struct {
	NameV, BinaryV            string
	Caps                      plan.Capabilities
	RenderFn                  func(plan.Step) ([]string, []plan.Warning, error) // nil renders the step type name followed by its names
	Rendered                  []plan.Step                                       // every Render argument, in call order
	Containers                []plan.ContainerState
	Networks, Volumes, Images []string
	Inspects                  []plan.ContainerState // returned by successive ParseInspect calls, the last one repeating
	ParseErr                  error                 // returned by every Parse method when non-nil
	ParsedInputs              map[string][][]byte   // every Parse input keyed by method name, such as "ParseContainers"

	inspected int
}

// NewFake returns a Fake named and executed as name that supports healthchecks.
func NewFake(name string) *Fake {
	return &Fake{NameV: name, BinaryV: name, Caps: plan.Capabilities{Healthcheck: true}, ParsedInputs: map[string][][]byte{}}
}

func (f *Fake) Name() string                    { return f.NameV }
func (f *Fake) Binary() string                  { return f.BinaryV }
func (f *Fake) Capabilities() plan.Capabilities { return f.Caps }

func (f *Fake) Render(step plan.Step) ([]string, []plan.Warning, error) {
	f.Rendered = append(f.Rendered, step)
	if f.RenderFn != nil {
		return f.RenderFn(step)
	}
	var names []string
	switch s := step.(type) {
	case plan.CreateNetwork:
		names = []string{s.Network.Name}
	case plan.CreateVolume:
		names = []string{s.Volume.Name}
	case plan.BuildImage:
		names = []string{s.Build.Tag}
	case plan.RunContainer:
		names = []string{s.Container.Name}
	case plan.StartContainers:
		names = s.Names
	case plan.StopContainers:
		names = s.Names
	case plan.RemoveContainers:
		names = s.Names
	case plan.RemoveNetwork:
		names = []string{s.Name}
	case plan.RemoveVolume:
		names = []string{s.Name}
	case plan.WaitContainer:
		if s.Condition == plan.ConditionHealthy && !f.Caps.Healthcheck {
			return nil, nil, fmt.Errorf("%s: cannot wait for %s to be healthy without healthcheck support", f.NameV, s.Name)
		}
		return nil, nil, nil
	case plan.ListContainers:
		names = []string{s.Project}
	case plan.ListNetworks, plan.ListVolumes, plan.ListImages:
	case plan.InspectContainer:
		names = []string{s.Name}
	case plan.Logs:
		names = []string{s.Name}
	default:
		return nil, nil, fmt.Errorf("%s: unsupported step %T", f.NameV, step)
	}
	return append([]string{reflect.TypeOf(step).Name()}, names...), nil, nil
}

func (f *Fake) ParseContainers(out []byte) ([]plan.ContainerState, error) {
	return parseList(f, "ParseContainers", out, f.Containers)
}

func (f *Fake) ParseNetworks(out []byte) ([]string, error) {
	return parseList(f, "ParseNetworks", out, f.Networks)
}

func (f *Fake) ParseVolumes(out []byte) ([]string, error) {
	return parseList(f, "ParseVolumes", out, f.Volumes)
}

func (f *Fake) ParseImages(out []byte) ([]string, error) {
	return parseList(f, "ParseImages", out, f.Images)
}

func (f *Fake) ParseInspect(out []byte) (plan.ContainerState, error) {
	blank, err := f.parse("ParseInspect", out)
	switch {
	case err != nil:
		return plan.ContainerState{}, err
	case blank:
		return plan.ContainerState{}, fmt.Errorf("%s: ParseInspect: blank input", f.NameV)
	case len(f.Inspects) == 0:
		return plan.ContainerState{}, fmt.Errorf("%s: ParseInspect: no Inspects configured", f.NameV)
	}
	s := f.Inspects[min(f.inspected, len(f.Inspects)-1)]
	f.inspected++
	return s, nil
}

func parseList[T any](f *Fake, method string, out []byte, configured []T) ([]T, error) {
	blank, err := f.parse(method, out)
	if err != nil {
		return nil, err
	}
	if blank || configured == nil {
		return []T{}, nil
	}
	return configured, nil
}

// parse records out under method, then reports whether it is blank or why it is rejected.
func (f *Fake) parse(method string, out []byte) (blank bool, err error) {
	if f.ParsedInputs == nil {
		f.ParsedInputs = map[string][][]byte{}
	}
	f.ParsedInputs[method] = append(f.ParsedInputs[method], out)
	switch {
	case f.ParseErr != nil:
		return false, f.ParseErr
	case len(bytes.TrimSpace(out)) == 0:
		return true, nil
	case !json.Valid(out):
		return false, fmt.Errorf("%s: %s: invalid JSON %q", f.NameV, method, out)
	}
	return false, nil
}

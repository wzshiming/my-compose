package plan

import "time"

type Plan struct {
	Steps []Step
}

type Step interface {
	step()
}

func (CreateNetwork) step()    {}
func (CreateVolume) step()     {}
func (BuildImage) step()       {}
func (RunContainer) step()     {}
func (StartContainers) step()  {}
func (StopContainers) step()   {}
func (RemoveContainers) step() {}
func (RemoveNetwork) step()    {}
func (RemoveVolume) step()     {}
func (WaitContainer) step()    {}
func (ListContainers) step()   {}
func (ListNetworks) step()     {}
func (ListVolumes) step()      {}
func (ListImages) step()       {}
func (InspectContainer) step() {}
func (Logs) step()             {}

type CreateNetwork struct {
	Network NetworkSpec
}

type CreateVolume struct {
	Volume VolumeSpec
}

type BuildImage struct {
	Build BuildSpec
}

type RunContainer struct {
	Container ContainerSpec
}

type StartContainers struct {
	Names []string
}

type StopContainers struct {
	Names   []string
	Timeout *time.Duration
}

type RemoveContainers struct {
	Names   []string
	Volumes bool
}

type RemoveNetwork struct {
	Name string
}

type RemoveVolume struct {
	Name string
}

type WaitContainer struct {
	Name      string
	Condition Condition
}

type ListContainers struct {
	Project string
}

type ListNetworks struct{}

type ListVolumes struct{}

type ListImages struct{}

type InspectContainer struct {
	Name string
}

type Logs struct {
	Name   string
	Follow bool
	Tail   string
}

type Condition string

const (
	// ConditionHealthy is met by Health healthy; Status exited or dead, Health unhealthy, or running with Health none (no healthcheck) fails it.
	ConditionHealthy Condition = "healthy"
	// ConditionCompleted is met by Status exited with ExitCode 0; exited non-zero or dead fails it.
	ConditionCompleted Condition = "completed"
)

// Capabilities lists optional engine features the planner may rely on.
type Capabilities struct {
	Healthcheck bool
}

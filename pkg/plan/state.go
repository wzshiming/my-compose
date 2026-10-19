package plan

type ContainerState struct {
	Name     string
	Service  string
	Number   int
	Image    string
	Status   string
	Health   string
	ExitCode int
	Ports    []Port
	Labels   map[string]string
	Command  []string
}

const (
	StatusCreated    = "created"
	StatusRunning    = "running"
	StatusPaused     = "paused"
	StatusRestarting = "restarting"
	StatusExited     = "exited"
	StatusDead       = "dead"
	StatusUnknown    = "unknown"
)

const (
	HealthNone      = "none"
	HealthStarting  = "starting"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)

// State lists existing engine resources.
type State struct {
	// Live means the state came from the engine; the zero State knows nothing (dry run).
	Live       bool
	Containers []ContainerState
	Networks   []string
	Volumes    []string
	Images     []string
}

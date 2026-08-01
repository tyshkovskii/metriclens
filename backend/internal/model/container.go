package model

type ContainerState string

const (
	ContainerStateRunning ContainerState = "running"
	ContainerStateExited  ContainerState = "exited"
	ContainerStateUnknown ContainerState = "unknown"
)

type DiscoveredContainer struct {
	ID string `json:"id"`
	// HistoryID is an internal identity used to retain metric history across
	// container recreations. The public container ID remains stable only for
	// the lifetime of one container and is still returned as ID.
	HistoryID      string            `json:"-"`
	Name           string            `json:"name"`
	Image          string            `json:"image"`
	State          ContainerState    `json:"state"`
	ComposeProject string            `json:"composeProject,omitempty"`
	ComposeService string            `json:"composeService,omitempty"`
	Networks       []string          `json:"networks"`
	ExposedPorts   []int             `json:"exposedPorts"`
	Labels         map[string]string `json:"labels"`
}

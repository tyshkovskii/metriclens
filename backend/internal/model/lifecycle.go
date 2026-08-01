package model

type LifecycleEventKind string

const (
	LifecycleEventAppeared         LifecycleEventKind = "appeared"
	LifecycleEventRecreated        LifecycleEventKind = "recreated"
	LifecycleEventDisappeared      LifecycleEventKind = "disappeared"
	LifecycleEventStatusTransition LifecycleEventKind = "status_transition"
)

// LifecycleEvent records a target snapshot change without exposing the
// internal stable identity used to correlate recreated Compose containers.
type LifecycleEvent struct {
	At            string             `json:"at"`
	Kind          LifecycleEventKind `json:"kind"`
	TargetID      string             `json:"targetId"`
	ServiceName   string             `json:"service"`
	ContainerName string             `json:"containerName,omitempty"`
	From          TargetStatus       `json:"from,omitempty"`
	To            TargetStatus       `json:"to,omitempty"`
	Error         string             `json:"error,omitempty"`
	HistoryID     string             `json:"-"`
}

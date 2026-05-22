package domain

import "time"

// WorkspaceEvent is a typed string constant representing a provisioning milestone.
type WorkspaceEvent string

const (
	EventCloneStarted     WorkspaceEvent = "clone_started"
	EventCloneCompleted   WorkspaceEvent = "clone_completed"
	EventWorktreeCreating WorkspaceEvent = "worktree_creating"
	EventWorktreeCreated  WorkspaceEvent = "worktree_created"
	EventProvisionFailed  WorkspaceEvent = "provision_failed"
)

// WorkspaceEventRecord is a single immutable event entry in the provisioning
// event stream.
type WorkspaceEventRecord struct {
	Event     WorkspaceEvent `json:"event"`
	Timestamp time.Time      `json:"timestamp"`
	Detail    string         `json:"detail,omitempty"`
}

// LifecycleStatus is the resolved business state projected from the event stream.
type LifecycleStatus string

const (
	LifecyclePending      LifecycleStatus = "pending"
	LifecycleProvisioning LifecycleStatus = "provisioning"
	LifecycleReady        LifecycleStatus = "ready"
	LifecycleFailed       LifecycleStatus = "failed"
)

// ResolveLifecycleStatus is a pure projection from an ordered event slice to a
// lifecycle status. Given the same events it always returns the same result.
func ResolveLifecycleStatus(events []WorkspaceEventRecord) LifecycleStatus {
	if len(events) == 0 {
		return LifecyclePending
	}
	latest := events[len(events)-1]
	switch latest.Event {
	case EventProvisionFailed:
		return LifecycleFailed
	case EventWorktreeCreated:
		return LifecycleReady
	default:
		return LifecycleProvisioning
	}
}

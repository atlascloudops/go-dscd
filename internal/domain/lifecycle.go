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
	EventCloneDetected    WorkspaceEvent = "clone_detected"

	// IDE events — informational only; these do NOT affect workspace lifecycle status.
	EventIDEStarted WorkspaceEvent = "ide_started"
	EventIDEReady   WorkspaceEvent = "ide_ready"
	EventIDEStopped WorkspaceEvent = "ide_stopped"
	EventIDEFailed  WorkspaceEvent = "ide_failed"

	// Hydrate events — informational only; these do NOT affect workspace lifecycle status.
	EventHydrateStarted   WorkspaceEvent = "hydrate_started"
	EventHydrateCompleted WorkspaceEvent = "hydrate_completed"
	EventHydrateSkipped   WorkspaceEvent = "hydrate_skipped"
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

// isIDEEvent returns true for events that are informational IDE lifecycle
// events. These never affect workspace lifecycle status.
func isIDEEvent(e WorkspaceEvent) bool {
	switch e {
	case EventIDEStarted, EventIDEReady, EventIDEStopped, EventIDEFailed:
		return true
	}
	return false
}

// isHydrateEvent returns true for informational hydration events.
// These never affect workspace lifecycle status.
func isHydrateEvent(e WorkspaceEvent) bool {
	switch e {
	case EventHydrateStarted, EventHydrateCompleted, EventHydrateSkipped:
		return true
	}
	return false
}

// isInfoEvent returns true for any informational event that does not affect
// workspace lifecycle status (IDE events and hydration events).
func isInfoEvent(e WorkspaceEvent) bool {
	return isIDEEvent(e) || isHydrateEvent(e)
}

// ResolveLifecycleStatus is a pure projection from an ordered event slice to a
// lifecycle status. Given the same events it always returns the same result.
// Informational events (IDE, hydration) are skipped — workspace status is
// determined solely by workspace/worktree events.
func ResolveLifecycleStatus(events []WorkspaceEventRecord) LifecycleStatus {
	if len(events) == 0 {
		return LifecyclePending
	}

	// Walk backwards to find the latest non-informational event.
	for i := len(events) - 1; i >= 0; i-- {
		if isInfoEvent(events[i].Event) {
			continue
		}
		switch events[i].Event {
		case EventProvisionFailed:
			return LifecycleFailed
		case EventWorktreeCreated, EventCloneDetected:
			return LifecycleReady
		default:
			return LifecycleProvisioning
		}
	}

	// All events are informational — treat as Pending (no workspace events yet).
	return LifecyclePending
}

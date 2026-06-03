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

	// Template provisioning events — informational; do NOT affect workspace lifecycle status.
	EventTemplateCloneStarted   WorkspaceEvent = "template_clone_started"
	EventTemplateCloneCompleted WorkspaceEvent = "template_clone_completed"
	EventTemplateReinitCompleted WorkspaceEvent = "template_reinit_completed"

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

// IDEEvent is a typed string constant representing an IDE lifecycle milestone.
// IDE events live in a separate stream from workspace events, enforced at
// compile time by distinct types.
type IDEEvent string

const (
	IDEEventStarted IDEEvent = "ide_started"
	IDEEventReady   IDEEvent = "ide_ready"
	IDEEventFailed  IDEEvent = "ide_failed"
	IDEEventStopped IDEEvent = "ide_stopped"
)

// IDEEventRecord is a single immutable event entry in the IDE event stream.
type IDEEventRecord struct {
	Event     IDEEvent  `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Detail    string    `json:"detail,omitempty"`
}

// Status is the resolved business state projected from an event stream.
type Status string

const (
	StatusPending      Status = "pending"
	StatusProvisioning Status = "provisioning"
	StatusReady        Status = "ready"
	StatusFailed       Status = "failed"
)

// StatusResolver is the interface for projecting an event stream into a Status.
// Workspace and IDE resolvers implement this with their respective event record types.
type StatusResolver[T any] interface {
	Resolve(events []T) Status
}

// WorkspaceStatusResolver resolves workspace lifecycle status from workspace events.
// Informational events (hydrate, template) are skipped — workspace status is
// determined solely by provisioning milestone events.
type WorkspaceStatusResolver struct{}

// Resolve projects an ordered workspace event slice into a Status.
func (WorkspaceStatusResolver) Resolve(events []WorkspaceEventRecord) Status {
	if len(events) == 0 {
		return StatusPending
	}

	// Walk backwards to find the latest status-affecting event.
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Event {
		case EventHydrateStarted, EventHydrateCompleted, EventHydrateSkipped,
			EventTemplateCloneStarted, EventTemplateCloneCompleted, EventTemplateReinitCompleted:
			// Informational — skip
			continue
		case EventProvisionFailed:
			return StatusFailed
		case EventWorktreeCreated, EventCloneDetected:
			return StatusReady
		default:
			return StatusProvisioning
		}
	}

	// All events are informational — treat as Pending (no provisioning events yet).
	return StatusPending
}

// IDEStatusResolver resolves IDE lifecycle status from IDE events.
type IDEStatusResolver struct{}

// Resolve projects an ordered IDE event slice into a Status.
func (IDEStatusResolver) Resolve(events []IDEEventRecord) Status {
	if len(events) == 0 {
		return StatusPending
	}

	latest := events[len(events)-1]
	switch latest.Event {
	case IDEEventReady:
		return StatusReady
	case IDEEventFailed:
		return StatusFailed
	case IDEEventStopped:
		return StatusPending
	case IDEEventStarted:
		return StatusProvisioning
	default:
		return StatusPending
	}
}

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

// workspaceInfoEvents are workspace events that do not affect lifecycle status.
var workspaceInfoEvents = map[string]bool{
	string(EventHydrateStarted):          true,
	string(EventHydrateCompleted):        true,
	string(EventHydrateSkipped):          true,
	string(EventTemplateCloneStarted):    true,
	string(EventTemplateCloneCompleted):  true,
	string(EventTemplateReinitCompleted): true,
}

// WorkspaceEventRecord is a single immutable event entry in the provisioning
// event stream. Retained for backward compatibility with existing provisioner
// and syncer code. New code should use EventRecord.
type WorkspaceEventRecord struct {
	Event     WorkspaceEvent `json:"event"`
	Timestamp time.Time      `json:"timestamp"`
	Detail    string         `json:"detail,omitempty"`
}

// ToEventRecord converts a WorkspaceEventRecord to a unified EventRecord.
// The scope name is left empty — callers should set it from context.
func (r WorkspaceEventRecord) ToEventRecord(scopeName string) EventRecord {
	return EventRecord{
		Scope:     EventScope{Kind: ScopeKindWorkspace, Name: scopeName},
		Event:     string(r.Event),
		Timestamp: r.Timestamp,
		Detail:    r.Detail,
	}
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
// Retained for backward compatibility with existing provisioner and syncer
// code. New code should use EventRecord.
type IDEEventRecord struct {
	Event     IDEEvent  `json:"event"`
	Timestamp time.Time `json:"timestamp"`
	Detail    string    `json:"detail,omitempty"`
}

// ToEventRecord converts an IDEEventRecord to a unified EventRecord.
// The scope name is left empty — callers should set it from context.
func (r IDEEventRecord) ToEventRecord(scopeName string) EventRecord {
	return EventRecord{
		Scope:     EventScope{Kind: ScopeKindIDE, Name: scopeName},
		Event:     string(r.Event),
		Timestamp: r.Timestamp,
		Detail:    r.Detail,
	}
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
type StatusResolver[T any] interface {
	Resolve(events []T) Status
}

// WorkspaceStatusResolver resolves workspace lifecycle status from events.
// Informational events (hydrate, template) are skipped — workspace status is
// determined solely by provisioning milestone events.
//
// Implements StatusResolver[EventRecord].
type WorkspaceStatusResolver struct{}

// Resolve projects an ordered event slice into a Status.
// Operates on unified EventRecord values, filtering by event name strings.
func (WorkspaceStatusResolver) Resolve(events []EventRecord) Status {
	if len(events) == 0 {
		return StatusPending
	}

	// Walk backwards to find the latest status-affecting event.
	for i := len(events) - 1; i >= 0; i-- {
		eventName := events[i].Event

		if workspaceInfoEvents[eventName] {
			// Informational — skip
			continue
		}

		switch eventName {
		case string(EventProvisionFailed):
			return StatusFailed
		case string(EventWorktreeCreated), string(EventCloneDetected):
			return StatusReady
		default:
			return StatusProvisioning
		}
	}

	// All events are informational — treat as Pending (no provisioning events yet).
	return StatusPending
}

// ResolveTyped projects an ordered workspace event record slice into a Status.
// This is a backward-compatible bridge for code that still uses WorkspaceEventRecord.
func (r WorkspaceStatusResolver) ResolveTyped(events []WorkspaceEventRecord) Status {
	unified := make([]EventRecord, len(events))
	for i, e := range events {
		unified[i] = EventRecord{
			Event:     string(e.Event),
			Timestamp: e.Timestamp,
			Detail:    e.Detail,
		}
	}
	return r.Resolve(unified)
}

// IDEStatusResolver resolves IDE lifecycle status from IDE events.
//
// Implements StatusResolver[EventRecord].
type IDEStatusResolver struct{}

// Resolve projects an ordered event slice into a Status.
// Operates on unified EventRecord values, using last-event-wins semantics.
func (IDEStatusResolver) Resolve(events []EventRecord) Status {
	if len(events) == 0 {
		return StatusPending
	}

	latest := events[len(events)-1]
	switch latest.Event {
	case string(IDEEventReady):
		return StatusReady
	case string(IDEEventFailed):
		return StatusFailed
	case string(IDEEventStopped):
		return StatusPending
	case string(IDEEventStarted):
		return StatusProvisioning
	default:
		return StatusPending
	}
}

// ResolveTyped projects an ordered IDE event record slice into a Status.
// This is a backward-compatible bridge for code that still uses IDEEventRecord.
func (r IDEStatusResolver) ResolveTyped(events []IDEEventRecord) Status {
	unified := make([]EventRecord, len(events))
	for i, e := range events {
		unified[i] = EventRecord{
			Event:     string(e.Event),
			Timestamp: e.Timestamp,
			Detail:    e.Detail,
		}
	}
	return r.Resolve(unified)
}

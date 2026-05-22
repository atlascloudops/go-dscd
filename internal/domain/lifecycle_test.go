package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestResolveLifecycleStatus_EmptyEvents(t *testing.T) {
	got := ResolveLifecycleStatus(nil)
	if got != LifecyclePending {
		t.Errorf("expected %q, got %q", LifecyclePending, got)
	}

	got = ResolveLifecycleStatus([]WorkspaceEventRecord{})
	if got != LifecyclePending {
		t.Errorf("expected %q for empty slice, got %q", LifecyclePending, got)
	}
}

func TestResolveLifecycleStatus_InProgress(t *testing.T) {
	cases := []WorkspaceEvent{
		EventCloneStarted,
		EventCloneCompleted,
		EventWorktreeCreating,
	}
	for _, evt := range cases {
		events := []WorkspaceEventRecord{
			{Event: evt, Timestamp: time.Now()},
		}
		got := ResolveLifecycleStatus(events)
		if got != LifecycleProvisioning {
			t.Errorf("event %q: expected %q, got %q", evt, LifecycleProvisioning, got)
		}
	}
}

func TestResolveLifecycleStatus_Ready(t *testing.T) {
	events := []WorkspaceEventRecord{
		{Event: EventCloneStarted, Timestamp: time.Now()},
		{Event: EventCloneCompleted, Timestamp: time.Now()},
		{Event: EventWorktreeCreating, Timestamp: time.Now()},
		{Event: EventWorktreeCreated, Timestamp: time.Now()},
	}
	got := ResolveLifecycleStatus(events)
	if got != LifecycleReady {
		t.Errorf("expected %q, got %q", LifecycleReady, got)
	}
}

func TestResolveLifecycleStatus_Failed(t *testing.T) {
	events := []WorkspaceEventRecord{
		{Event: EventCloneStarted, Timestamp: time.Now()},
		{Event: EventProvisionFailed, Timestamp: time.Now(), Detail: "clone timed out"},
	}
	got := ResolveLifecycleStatus(events)
	if got != LifecycleFailed {
		t.Errorf("expected %q, got %q", LifecycleFailed, got)
	}
}

func TestWorkspaceEventRecord_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	record := WorkspaceEventRecord{
		Event:     EventCloneStarted,
		Timestamp: ts,
		Detail:    "starting bare clone",
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got WorkspaceEventRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Event != record.Event {
		t.Errorf("event: expected %q, got %q", record.Event, got.Event)
	}
	if !got.Timestamp.Equal(record.Timestamp) {
		t.Errorf("timestamp: expected %v, got %v", record.Timestamp, got.Timestamp)
	}
	if got.Detail != record.Detail {
		t.Errorf("detail: expected %q, got %q", record.Detail, got.Detail)
	}
}

func TestWorkspaceInstance_EventsJSON(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	inst := WorkspaceInstance{
		State:     StatePending,
		Lifecycle: LifecycleProvisioning,
		Events: []WorkspaceEventRecord{
			{Event: EventCloneStarted, Timestamp: ts},
		},
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got WorkspaceInstance
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got.Events))
	}
	if got.Events[0].Event != EventCloneStarted {
		t.Errorf("event: expected %q, got %q", EventCloneStarted, got.Events[0].Event)
	}
	if got.Lifecycle != LifecycleProvisioning {
		t.Errorf("lifecycle: expected %q, got %q", LifecycleProvisioning, got.Lifecycle)
	}
}

func TestWorkspaceInstance_EmptyEventsOmitted(t *testing.T) {
	inst := WorkspaceInstance{
		State: StatePending,
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Events and Lifecycle should be omitted from JSON when empty/zero
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["events"]; ok {
		t.Error("expected events to be omitted from JSON when nil")
	}
	if _, ok := raw["lifecycle"]; ok {
		t.Error("expected lifecycle to be omitted from JSON when empty")
	}
}

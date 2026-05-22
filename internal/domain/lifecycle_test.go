package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// --- WorkspaceStatusResolver tests ---

func TestWorkspaceStatusResolver_EmptyEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	got := r.Resolve(nil)
	if got != StatusPending {
		t.Errorf("expected %q, got %q", StatusPending, got)
	}

	got = r.Resolve([]WorkspaceEventRecord{})
	if got != StatusPending {
		t.Errorf("expected %q for empty slice, got %q", StatusPending, got)
	}
}

func TestWorkspaceStatusResolver_InProgress(t *testing.T) {
	var r WorkspaceStatusResolver
	cases := []WorkspaceEvent{
		EventCloneStarted,
		EventCloneCompleted,
		EventWorktreeCreating,
	}
	for _, evt := range cases {
		events := []WorkspaceEventRecord{
			{Event: evt, Timestamp: time.Now()},
		}
		got := r.Resolve(events)
		if got != StatusProvisioning {
			t.Errorf("event %q: expected %q, got %q", evt, StatusProvisioning, got)
		}
	}
}

func TestWorkspaceStatusResolver_Ready(t *testing.T) {
	var r WorkspaceStatusResolver
	events := []WorkspaceEventRecord{
		{Event: EventCloneStarted, Timestamp: time.Now()},
		{Event: EventCloneCompleted, Timestamp: time.Now()},
		{Event: EventWorktreeCreating, Timestamp: time.Now()},
		{Event: EventWorktreeCreated, Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

func TestWorkspaceStatusResolver_Failed(t *testing.T) {
	var r WorkspaceStatusResolver
	events := []WorkspaceEventRecord{
		{Event: EventCloneStarted, Timestamp: time.Now()},
		{Event: EventProvisionFailed, Timestamp: time.Now(), Detail: "clone timed out"},
	}
	got := r.Resolve(events)
	if got != StatusFailed {
		t.Errorf("expected %q, got %q", StatusFailed, got)
	}
}

func TestWorkspaceStatusResolver_IgnoresHydrateEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	baseEvents := []WorkspaceEventRecord{
		{Event: EventCloneStarted, Timestamp: time.Now()},
		{Event: EventCloneCompleted, Timestamp: time.Now()},
		{Event: EventWorktreeCreating, Timestamp: time.Now()},
		{Event: EventWorktreeCreated, Timestamp: time.Now()},
	}

	hydrateEvents := []WorkspaceEvent{
		EventHydrateStarted,
		EventHydrateCompleted,
		EventHydrateSkipped,
	}

	for _, he := range hydrateEvents {
		events := append([]WorkspaceEventRecord{}, baseEvents...)
		events = append(events, WorkspaceEventRecord{Event: he, Timestamp: time.Now()})
		got := r.Resolve(events)
		if got != StatusReady {
			t.Errorf("after %q: expected %q, got %q", he, StatusReady, got)
		}
	}
}

func TestWorkspaceStatusResolver_HydrateAfterFailed(t *testing.T) {
	var r WorkspaceStatusResolver
	events := []WorkspaceEventRecord{
		{Event: EventCloneStarted, Timestamp: time.Now()},
		{Event: EventProvisionFailed, Timestamp: time.Now(), Detail: "clone error"},
		{Event: EventHydrateSkipped, Timestamp: time.Now(), Detail: "fetch failed"},
	}
	got := r.Resolve(events)
	if got != StatusFailed {
		t.Errorf("expected %q, got %q", StatusFailed, got)
	}
}

func TestWorkspaceStatusResolver_OnlyHydrateEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	events := []WorkspaceEventRecord{
		{Event: EventHydrateStarted, Timestamp: time.Now()},
		{Event: EventHydrateCompleted, Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusPending {
		t.Errorf("expected %q for only-hydrate events, got %q", StatusPending, got)
	}
}

func TestWorkspaceStatusResolver_MixedInfoEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	events := []WorkspaceEventRecord{
		{Event: EventWorktreeCreated, Timestamp: time.Now()},
		{Event: EventHydrateStarted, Timestamp: time.Now()},
		{Event: EventHydrateCompleted, Timestamp: time.Now()},
		{Event: EventGitCredentialsExist, Timestamp: time.Now()},
		{Event: EventHydrateSkipped, Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

func TestWorkspaceStatusResolver_GitCredentialsExistIsInformational(t *testing.T) {
	var r WorkspaceStatusResolver

	// git_credentials_exist after Ready — status stays Ready
	events := []WorkspaceEventRecord{
		{Event: EventWorktreeCreated, Timestamp: time.Now()},
		{Event: EventGitCredentialsExist, Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q after git_credentials_exist, got %q", StatusReady, got)
	}

	// Only git_credentials_exist — status is Pending
	events2 := []WorkspaceEventRecord{
		{Event: EventGitCredentialsExist, Timestamp: time.Now()},
	}
	got2 := r.Resolve(events2)
	if got2 != StatusPending {
		t.Errorf("expected %q for only git_credentials_exist, got %q", StatusPending, got2)
	}
}

func TestWorkspaceStatusResolver_CloneDetected(t *testing.T) {
	var r WorkspaceStatusResolver
	events := []WorkspaceEventRecord{
		{Event: EventCloneDetected, Timestamp: time.Now(), Detail: "detected by sync"},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

// --- IDEStatusResolver tests ---

func TestIDEStatusResolver_EmptyEvents(t *testing.T) {
	var r IDEStatusResolver
	got := r.Resolve(nil)
	if got != StatusPending {
		t.Errorf("expected %q, got %q", StatusPending, got)
	}

	got = r.Resolve([]IDEEventRecord{})
	if got != StatusPending {
		t.Errorf("expected %q for empty slice, got %q", StatusPending, got)
	}
}

func TestIDEStatusResolver_Started(t *testing.T) {
	var r IDEStatusResolver
	events := []IDEEventRecord{
		{Event: IDEEventStarted, Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusProvisioning {
		t.Errorf("expected %q, got %q", StatusProvisioning, got)
	}
}

func TestIDEStatusResolver_Ready(t *testing.T) {
	var r IDEStatusResolver
	events := []IDEEventRecord{
		{Event: IDEEventStarted, Timestamp: time.Now()},
		{Event: IDEEventReady, Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

func TestIDEStatusResolver_Failed(t *testing.T) {
	var r IDEStatusResolver
	events := []IDEEventRecord{
		{Event: IDEEventStarted, Timestamp: time.Now()},
		{Event: IDEEventFailed, Timestamp: time.Now(), Detail: "systemd error"},
	}
	got := r.Resolve(events)
	if got != StatusFailed {
		t.Errorf("expected %q, got %q", StatusFailed, got)
	}
}

func TestIDEStatusResolver_Stopped(t *testing.T) {
	var r IDEStatusResolver
	events := []IDEEventRecord{
		{Event: IDEEventStarted, Timestamp: time.Now()},
		{Event: IDEEventReady, Timestamp: time.Now()},
		{Event: IDEEventStopped, Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusPending {
		t.Errorf("expected %q after stop, got %q", StatusPending, got)
	}
}

// --- Event record JSON tests ---

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

func TestIDEEventRecord_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	record := IDEEventRecord{
		Event:     IDEEventStarted,
		Timestamp: ts,
		Detail:    "port=9100",
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got IDEEventRecord
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

// --- WorkspaceInstance JSON tests ---

func TestWorkspaceInstance_EventsJSON(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	inst := WorkspaceInstance{
		Status: StatusProvisioning,
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
	if got.Status != StatusProvisioning {
		t.Errorf("status: expected %q, got %q", StatusProvisioning, got.Status)
	}
}

func TestWorkspaceInstance_IDEInstanceEventsJSON(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	inst := WorkspaceInstance{
		Status: StatusReady,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Events: []IDEEventRecord{
				{Event: IDEEventStarted, Timestamp: ts},
				{Event: IDEEventReady, Timestamp: ts},
			},
			Status: StatusReady,
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

	if got.IDE == nil {
		t.Fatal("IDE should not be nil after round-trip")
	}
	if len(got.IDE.Events) != 2 {
		t.Fatalf("expected 2 IDE events, got %d", len(got.IDE.Events))
	}
	if got.IDE.Status != StatusReady {
		t.Errorf("IDE.Status: expected %q, got %q", StatusReady, got.IDE.Status)
	}
}

func TestWorkspaceInstance_EmptyEventsOmitted(t *testing.T) {
	inst := WorkspaceInstance{}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Events, Status, IDE should be omitted from JSON when empty/zero/nil
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["events"]; ok {
		t.Error("expected events to be omitted from JSON when nil")
	}
	if _, ok := raw["status"]; ok {
		t.Error("expected status to be omitted from JSON when empty")
	}
	if _, ok := raw["ide"]; ok {
		t.Error("expected ide to be omitted from JSON when nil")
	}
}

// --- StatusResolver interface compile-time checks ---

func TestWorkspaceStatusResolver_ImplementsInterface(t *testing.T) {
	var _ StatusResolver[WorkspaceEventRecord] = WorkspaceStatusResolver{}
}

func TestIDEStatusResolver_ImplementsInterface(t *testing.T) {
	var _ StatusResolver[IDEEventRecord] = IDEStatusResolver{}
}

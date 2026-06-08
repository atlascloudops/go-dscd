package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// --- WorkspaceStatusResolver backward-compat tests (ResolveTyped) ---

func TestWorkspaceStatusResolver_EmptyEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	got := r.ResolveTyped(nil)
	if got != StatusPending {
		t.Errorf("expected %q, got %q", StatusPending, got)
	}

	got = r.ResolveTyped([]WorkspaceEventRecord{})
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
		got := r.ResolveTyped(events)
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
	got := r.ResolveTyped(events)
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
	got := r.ResolveTyped(events)
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
		got := r.ResolveTyped(events)
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
	got := r.ResolveTyped(events)
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
	got := r.ResolveTyped(events)
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
		{Event: EventHydrateSkipped, Timestamp: time.Now()},
	}
	got := r.ResolveTyped(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

func TestWorkspaceStatusResolver_CloneDetected(t *testing.T) {
	var r WorkspaceStatusResolver
	events := []WorkspaceEventRecord{
		{Event: EventCloneDetected, Timestamp: time.Now(), Detail: "detected by sync"},
	}
	got := r.ResolveTyped(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

// --- IDEStatusResolver backward-compat tests (ResolveTyped) ---

func TestIDEStatusResolver_EmptyEvents(t *testing.T) {
	var r IDEStatusResolver
	got := r.ResolveTyped(nil)
	if got != StatusPending {
		t.Errorf("expected %q, got %q", StatusPending, got)
	}

	got = r.ResolveTyped([]IDEEventRecord{})
	if got != StatusPending {
		t.Errorf("expected %q for empty slice, got %q", StatusPending, got)
	}
}

func TestIDEStatusResolver_Started(t *testing.T) {
	var r IDEStatusResolver
	events := []IDEEventRecord{
		{Event: IDEEventStarted, Timestamp: time.Now()},
	}
	got := r.ResolveTyped(events)
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
	got := r.ResolveTyped(events)
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
	got := r.ResolveTyped(events)
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
	got := r.ResolveTyped(events)
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

// --- Workspace JSON tests ---

func TestWorkspace_EventsJSON(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	scope := EventScope{Kind: ScopeKindWorkspace, Name: "infra"}
	inst := Workspace{
		Spec:   WorkspaceSpec{Name: "infra"},
		Status: StatusProvisioning,
		Events: []EventRecord{
			{Scope: scope, Event: string(EventCloneStarted), Timestamp: ts},
		},
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Workspace
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got.Events))
	}
	if got.Events[0].Event != string(EventCloneStarted) {
		t.Errorf("event: expected %q, got %q", EventCloneStarted, got.Events[0].Event)
	}
	if got.Events[0].Scope.Kind != ScopeKindWorkspace {
		t.Errorf("scope.Kind: expected %q, got %q", ScopeKindWorkspace, got.Events[0].Scope.Kind)
	}
	if got.Status != StatusProvisioning {
		t.Errorf("status: expected %q, got %q", StatusProvisioning, got.Status)
	}
}

func TestWorkspace_IDEInstanceEventsJSON(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	scope := EventScope{Kind: ScopeKindIDE, Name: "infra"}
	inst := Workspace{
		Status: StatusReady,
		IDE: &IDEInstance{
			Name:    "infra",
			Adapter: "openvscode-server",
			Port:    9100,
			Events: []EventRecord{
				{Scope: scope, Event: string(IDEEventStarted), Timestamp: ts},
				{Scope: scope, Event: string(IDEEventReady), Timestamp: ts},
			},
			Status: StatusReady,
		},
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Workspace
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

func TestWorkspace_EmptyEventsOmitted(t *testing.T) {
	inst := Workspace{}

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
	var _ StatusResolver[EventRecord] = WorkspaceStatusResolver{}
}

func TestIDEStatusResolver_ImplementsInterface(t *testing.T) {
	var _ StatusResolver[EventRecord] = IDEStatusResolver{}
}

// --- Workspace.RecordEvent tests ---

func TestWorkspace_RecordEvent_AppendsWithCorrectScope(t *testing.T) {
	w := &Workspace{
		Spec: WorkspaceSpec{Name: "infra"},
	}

	w.RecordEvent(EventCloneStarted, "https://github.com/org/repo.git")

	if len(w.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(w.Events))
	}

	ev := w.Events[0]
	if ev.Scope.Kind != ScopeKindWorkspace {
		t.Errorf("scope.Kind: expected %q, got %q", ScopeKindWorkspace, ev.Scope.Kind)
	}
	if ev.Scope.Name != "infra" {
		t.Errorf("scope.Name: expected %q, got %q", "infra", ev.Scope.Name)
	}
	if ev.Event != string(EventCloneStarted) {
		t.Errorf("event: expected %q, got %q", EventCloneStarted, ev.Event)
	}
	if ev.Detail != "https://github.com/org/repo.git" {
		t.Errorf("detail: expected clone URL, got %q", ev.Detail)
	}
	if ev.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestWorkspace_RecordEvent_ProjectsStatus(t *testing.T) {
	w := &Workspace{
		Spec: WorkspaceSpec{Name: "infra"},
	}

	w.RecordEvent(EventCloneStarted, "")
	if w.Status != StatusProvisioning {
		t.Errorf("after clone_started: expected %q, got %q", StatusProvisioning, w.Status)
	}

	w.RecordEvent(EventCloneCompleted, "")
	if w.Status != StatusProvisioning {
		t.Errorf("after clone_completed: expected %q, got %q", StatusProvisioning, w.Status)
	}

	w.RecordEvent(EventWorktreeCreated, "main")
	if w.Status != StatusReady {
		t.Errorf("after worktree_created: expected %q, got %q", StatusReady, w.Status)
	}
}

func TestWorkspace_RecordEvent_FailedStatus(t *testing.T) {
	w := &Workspace{
		Spec: WorkspaceSpec{Name: "infra"},
	}

	w.RecordEvent(EventCloneStarted, "")
	w.RecordEvent(EventProvisionFailed, "clone timed out")

	if w.Status != StatusFailed {
		t.Errorf("expected %q, got %q", StatusFailed, w.Status)
	}
	if len(w.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(w.Events))
	}
}

func TestWorkspace_RecordEvent_MultipleEvents(t *testing.T) {
	w := &Workspace{
		Spec: WorkspaceSpec{Name: "infra/feat"},
	}

	w.RecordEvent(EventCloneStarted, "url")
	w.RecordEvent(EventCloneCompleted, "")
	w.RecordEvent(EventWorktreeCreating, "main")
	w.RecordEvent(EventWorktreeCreated, "main")
	w.RecordEvent(EventHydrateStarted, "")
	w.RecordEvent(EventHydrateCompleted, "main")

	if len(w.Events) != 6 {
		t.Fatalf("expected 6 events, got %d", len(w.Events))
	}
	// All events should have the same scope
	for i, ev := range w.Events {
		if ev.Scope.Kind != ScopeKindWorkspace || ev.Scope.Name != "infra/feat" {
			t.Errorf("event[%d] scope: expected workspace:infra/feat, got %s", i, ev.Scope.String())
		}
	}
	// Status should be ready (hydrate events are informational)
	if w.Status != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, w.Status)
	}
}

// --- Workspace backward-compatible JSON deserialization ---

func TestWorkspace_UnmarshalJSON_OldFormat(t *testing.T) {
	// Simulate old state.json with WorkspaceEventRecord format (no scope field)
	oldJSON := `{
		"spec": {"name": "infra", "vcs": {"host": "github.com", "repo": "org/repo", "branch": "main"}, "owner": "user"},
		"events": [
			{"event": "clone_started", "timestamp": "2026-05-21T10:00:00Z", "detail": "https://github.com/org/repo.git"},
			{"event": "clone_completed", "timestamp": "2026-05-21T10:01:00Z"},
			{"event": "worktree_created", "timestamp": "2026-05-21T10:02:00Z", "detail": "main"}
		],
		"status": "ready"
	}`

	var w Workspace
	if err := json.Unmarshal([]byte(oldJSON), &w); err != nil {
		t.Fatalf("unmarshal old format: %v", err)
	}

	if len(w.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(w.Events))
	}

	// Verify events were converted to EventRecord with workspace scope
	for i, ev := range w.Events {
		if ev.Scope.Kind != ScopeKindWorkspace {
			t.Errorf("event[%d] scope.Kind: expected %q, got %q", i, ScopeKindWorkspace, ev.Scope.Kind)
		}
		if ev.Scope.Name != "infra" {
			t.Errorf("event[%d] scope.Name: expected %q, got %q", i, "infra", ev.Scope.Name)
		}
	}

	if w.Events[0].Event != string(EventCloneStarted) {
		t.Errorf("event[0]: expected %q, got %q", EventCloneStarted, w.Events[0].Event)
	}
	if w.Events[0].Detail != "https://github.com/org/repo.git" {
		t.Errorf("event[0] detail: expected clone URL, got %q", w.Events[0].Detail)
	}
}

func TestWorkspace_UnmarshalJSON_NewFormat(t *testing.T) {
	// New format with scope field
	newJSON := `{
		"spec": {"name": "infra", "vcs": {"host": "github.com", "repo": "org/repo", "branch": "main"}, "owner": "user"},
		"events": [
			{"scope": "workspace:infra", "event": "clone_started", "timestamp": "2026-05-21T10:00:00Z"}
		],
		"status": "provisioning"
	}`

	var w Workspace
	if err := json.Unmarshal([]byte(newJSON), &w); err != nil {
		t.Fatalf("unmarshal new format: %v", err)
	}

	if len(w.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(w.Events))
	}
	if w.Events[0].Scope.Kind != ScopeKindWorkspace {
		t.Errorf("scope.Kind: expected %q, got %q", ScopeKindWorkspace, w.Events[0].Scope.Kind)
	}
	if w.Events[0].Scope.Name != "infra" {
		t.Errorf("scope.Name: expected %q, got %q", "infra", w.Events[0].Scope.Name)
	}
}

package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// --- EventScope validation tests ---

func TestNewEventScope_Valid(t *testing.T) {
	cases := []struct {
		kind, name string
	}{
		{ScopeKindWorkspace, "infra"},
		{ScopeKindIDE, "infra"},
		{ScopeKindCredentials, "jperez"},
		{"workspace", "infra/feat"},
		{"custom123", "some-name"},
	}
	for _, tc := range cases {
		scope, err := NewEventScope(tc.kind, tc.name)
		if err != nil {
			t.Errorf("NewEventScope(%q, %q) unexpected error: %v", tc.kind, tc.name, err)
			continue
		}
		if scope.Kind != tc.kind {
			t.Errorf("Kind: expected %q, got %q", tc.kind, scope.Kind)
		}
		if scope.Name != tc.name {
			t.Errorf("Name: expected %q, got %q", tc.name, scope.Name)
		}
	}
}

func TestNewEventScope_EmptyKind(t *testing.T) {
	_, err := NewEventScope("", "infra")
	if err == nil {
		t.Error("expected error for empty kind")
	}
}

func TestNewEventScope_EmptyName(t *testing.T) {
	_, err := NewEventScope("workspace", "")
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestNewEventScope_InvalidKindChars(t *testing.T) {
	invalid := []string{
		"Work",       // uppercase
		"work:space", // colon
		"work/space", // slash
		"work space", // space
		"work_space", // underscore
		"work-space", // hyphen
	}
	for _, kind := range invalid {
		_, err := NewEventScope(kind, "test")
		if err == nil {
			t.Errorf("expected error for kind %q", kind)
		}
	}
}

func TestEventScope_String(t *testing.T) {
	scope := EventScope{Kind: "workspace", Name: "infra"}
	got := scope.String()
	if got != "workspace:infra" {
		t.Errorf("expected %q, got %q", "workspace:infra", got)
	}

	scope2 := EventScope{Kind: "ide", Name: "infra/feat"}
	got2 := scope2.String()
	if got2 != "ide:infra/feat" {
		t.Errorf("expected %q, got %q", "ide:infra/feat", got2)
	}
}

// --- EventScope JSON round-trip tests ---

func TestEventScope_JSONRoundTrip(t *testing.T) {
	scope, _ := NewEventScope("workspace", "infra")

	data, err := json.Marshal(scope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Should serialize as a plain string
	expected := `"workspace:infra"`
	if string(data) != expected {
		t.Errorf("marshal: expected %s, got %s", expected, string(data))
	}

	var got EventScope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != "workspace" || got.Name != "infra" {
		t.Errorf("round-trip: expected workspace:infra, got %s:%s", got.Kind, got.Name)
	}
}

func TestEventScope_JSONWithSlashInName(t *testing.T) {
	scope := EventScope{Kind: "workspace", Name: "infra/feat"}

	data, err := json.Marshal(scope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	expected := `"workspace:infra/feat"`
	if string(data) != expected {
		t.Errorf("marshal: expected %s, got %s", expected, string(data))
	}

	var got EventScope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != "workspace" || got.Name != "infra/feat" {
		t.Errorf("round-trip: expected workspace:infra/feat, got %s:%s", got.Kind, got.Name)
	}
}

func TestEventScope_UnmarshalInvalid(t *testing.T) {
	cases := []string{
		`"nocolon"`,    // no colon
		`":name"`,      // empty kind
		`"kind:"`,      // empty name
		`42`,           // not a string
		`""`,           // empty string
	}
	for _, raw := range cases {
		var s EventScope
		err := json.Unmarshal([]byte(raw), &s)
		if err == nil {
			t.Errorf("expected error for %s", raw)
		}
	}
}

// --- EventRecord JSON tests ---

func TestEventRecord_JSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra"},
		Event:     "clone_started",
		Timestamp: ts,
		Detail:    "starting bare clone",
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the scope is serialized as a "kind:name" string
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	scopeVal, ok := raw["scope"].(string)
	if !ok {
		t.Fatalf("scope should be a string, got %T", raw["scope"])
	}
	if scopeVal != "workspace:infra" {
		t.Errorf("scope: expected %q, got %q", "workspace:infra", scopeVal)
	}

	var got EventRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Scope.Kind != "workspace" || got.Scope.Name != "infra" {
		t.Errorf("scope: expected workspace:infra, got %s:%s", got.Scope.Kind, got.Scope.Name)
	}
	if got.Event != "clone_started" {
		t.Errorf("event: expected %q, got %q", "clone_started", got.Event)
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("timestamp: expected %v, got %v", ts, got.Timestamp)
	}
	if got.Detail != "starting bare clone" {
		t.Errorf("detail: expected %q, got %q", "starting bare clone", got.Detail)
	}
}

func TestEventRecord_DetailOmitEmpty(t *testing.T) {
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra"},
		Event:     "clone_completed",
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["detail"]; ok {
		t.Error("expected detail to be omitted when empty")
	}
}

// --- StatusResolver with EventRecord tests ---

func TestWorkspaceStatusResolver_EventRecord_EmptyEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	got := r.Resolve(nil)
	if got != StatusPending {
		t.Errorf("expected %q, got %q", StatusPending, got)
	}

	got = r.Resolve([]EventRecord{})
	if got != StatusPending {
		t.Errorf("expected %q for empty slice, got %q", StatusPending, got)
	}
}

func TestWorkspaceStatusResolver_EventRecord_InProgress(t *testing.T) {
	var r WorkspaceStatusResolver
	scope := EventScope{Kind: ScopeKindWorkspace, Name: "infra"}
	cases := []string{
		string(EventCloneStarted),
		string(EventCloneCompleted),
		string(EventWorktreeCreating),
	}
	for _, evt := range cases {
		events := []EventRecord{
			{Scope: scope, Event: evt, Timestamp: time.Now()},
		}
		got := r.Resolve(events)
		if got != StatusProvisioning {
			t.Errorf("event %q: expected %q, got %q", evt, StatusProvisioning, got)
		}
	}
}

func TestWorkspaceStatusResolver_EventRecord_Ready(t *testing.T) {
	var r WorkspaceStatusResolver
	scope := EventScope{Kind: ScopeKindWorkspace, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(EventCloneStarted), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventCloneCompleted), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventWorktreeCreating), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventWorktreeCreated), Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

func TestWorkspaceStatusResolver_EventRecord_Failed(t *testing.T) {
	var r WorkspaceStatusResolver
	scope := EventScope{Kind: ScopeKindWorkspace, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(EventCloneStarted), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventProvisionFailed), Timestamp: time.Now(), Detail: "clone timed out"},
	}
	got := r.Resolve(events)
	if got != StatusFailed {
		t.Errorf("expected %q, got %q", StatusFailed, got)
	}
}

func TestWorkspaceStatusResolver_EventRecord_IgnoresInfoEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	scope := EventScope{Kind: ScopeKindWorkspace, Name: "infra"}
	baseEvents := []EventRecord{
		{Scope: scope, Event: string(EventCloneStarted), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventCloneCompleted), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventWorktreeCreating), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventWorktreeCreated), Timestamp: time.Now()},
	}

	infoEvents := []string{
		string(EventHydrateStarted),
		string(EventHydrateCompleted),
		string(EventHydrateSkipped),
		string(EventTemplateCloneStarted),
		string(EventTemplateCloneCompleted),
		string(EventTemplateReinitCompleted),
	}

	for _, he := range infoEvents {
		events := append([]EventRecord{}, baseEvents...)
		events = append(events, EventRecord{Scope: scope, Event: he, Timestamp: time.Now()})
		got := r.Resolve(events)
		if got != StatusReady {
			t.Errorf("after %q: expected %q, got %q", he, StatusReady, got)
		}
	}
}

func TestWorkspaceStatusResolver_EventRecord_OnlyInfoEvents(t *testing.T) {
	var r WorkspaceStatusResolver
	scope := EventScope{Kind: ScopeKindWorkspace, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(EventHydrateStarted), Timestamp: time.Now()},
		{Scope: scope, Event: string(EventHydrateCompleted), Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusPending {
		t.Errorf("expected %q, got %q", StatusPending, got)
	}
}

func TestWorkspaceStatusResolver_EventRecord_CloneDetected(t *testing.T) {
	var r WorkspaceStatusResolver
	scope := EventScope{Kind: ScopeKindWorkspace, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(EventCloneDetected), Timestamp: time.Now(), Detail: "detected by sync"},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

func TestIDEStatusResolver_EventRecord_EmptyEvents(t *testing.T) {
	var r IDEStatusResolver
	got := r.Resolve(nil)
	if got != StatusPending {
		t.Errorf("expected %q, got %q", StatusPending, got)
	}

	got = r.Resolve([]EventRecord{})
	if got != StatusPending {
		t.Errorf("expected %q for empty slice, got %q", StatusPending, got)
	}
}

func TestIDEStatusResolver_EventRecord_Started(t *testing.T) {
	var r IDEStatusResolver
	scope := EventScope{Kind: ScopeKindIDE, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(IDEEventStarted), Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusProvisioning {
		t.Errorf("expected %q, got %q", StatusProvisioning, got)
	}
}

func TestIDEStatusResolver_EventRecord_Ready(t *testing.T) {
	var r IDEStatusResolver
	scope := EventScope{Kind: ScopeKindIDE, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(IDEEventStarted), Timestamp: time.Now()},
		{Scope: scope, Event: string(IDEEventReady), Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusReady {
		t.Errorf("expected %q, got %q", StatusReady, got)
	}
}

func TestIDEStatusResolver_EventRecord_Failed(t *testing.T) {
	var r IDEStatusResolver
	scope := EventScope{Kind: ScopeKindIDE, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(IDEEventStarted), Timestamp: time.Now()},
		{Scope: scope, Event: string(IDEEventFailed), Timestamp: time.Now(), Detail: "systemd error"},
	}
	got := r.Resolve(events)
	if got != StatusFailed {
		t.Errorf("expected %q, got %q", StatusFailed, got)
	}
}

func TestIDEStatusResolver_EventRecord_Stopped(t *testing.T) {
	var r IDEStatusResolver
	scope := EventScope{Kind: ScopeKindIDE, Name: "infra"}
	events := []EventRecord{
		{Scope: scope, Event: string(IDEEventStarted), Timestamp: time.Now()},
		{Scope: scope, Event: string(IDEEventReady), Timestamp: time.Now()},
		{Scope: scope, Event: string(IDEEventStopped), Timestamp: time.Now()},
	}
	got := r.Resolve(events)
	if got != StatusPending {
		t.Errorf("expected %q after stop, got %q", StatusPending, got)
	}
}

// --- StatusResolver interface compile-time checks with EventRecord ---

func TestWorkspaceStatusResolver_ImplementsEventRecordInterface(t *testing.T) {
	var _ StatusResolver[EventRecord] = WorkspaceStatusResolver{}
}

func TestIDEStatusResolver_ImplementsEventRecordInterface(t *testing.T) {
	var _ StatusResolver[EventRecord] = IDEStatusResolver{}
}

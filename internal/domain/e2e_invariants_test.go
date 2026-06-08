package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// TestProductionHardeningInvariants validates the structural invariants
// established by the dscd production hardening epic. These tests ensure
// that aggregate models, event framework, path constants, and state schema
// remain aligned with the epic's acceptance criteria.

func TestDefaultActivityLogPath(t *testing.T) {
	// Activity log must live under /var/lib/dscd/, not /opt/dsc/.
	expected := "/var/lib/dscd/activity.log"
	if DefaultActivityLogPath != expected {
		t.Errorf("DefaultActivityLogPath = %q, want %q", DefaultActivityLogPath, expected)
	}
}

func TestWorkspaceAggregate_RecordEvent_SetsScope(t *testing.T) {
	ws := &Workspace{
		Spec: WorkspaceSpec{Name: "infra"},
	}
	ws.RecordEvent(EventCloneStarted, "testing")

	if len(ws.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ws.Events))
	}

	ev := ws.Events[0]
	if ev.Scope.Kind != ScopeKindWorkspace {
		t.Errorf("scope kind = %q, want %q", ev.Scope.Kind, ScopeKindWorkspace)
	}
	if ev.Scope.Name != "infra" {
		t.Errorf("scope name = %q, want %q", ev.Scope.Name, "infra")
	}
	if ev.Event != string(EventCloneStarted) {
		t.Errorf("event = %q, want %q", ev.Event, EventCloneStarted)
	}
}

func TestCredentialAggregate_RecordEvent_SetsScope(t *testing.T) {
	cs := &CredentialState{Owner: "jperez"}
	cs.RecordEvent(CredEventGitWritten, "github.com")

	if len(cs.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cs.Events))
	}

	ev := cs.Events[0]
	if ev.Scope.Kind != ScopeKindCredentials {
		t.Errorf("scope kind = %q, want %q", ev.Scope.Kind, ScopeKindCredentials)
	}
	if ev.Scope.Name != "jperez" {
		t.Errorf("scope name = %q, want %q", ev.Scope.Name, "jperez")
	}
}

func TestIDEInstance_RecordEvent_SetsScope(t *testing.T) {
	ide := &IDEInstance{
		Adapter: "openvscode-server",
		Name:    "infra",
	}
	ide.RecordEvent(IDEEventStarted, "port 3000")

	if len(ide.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ide.Events))
	}

	ev := ide.Events[0]
	if ev.Scope.Kind != ScopeKindIDE {
		t.Errorf("scope kind = %q, want %q", ev.Scope.Kind, ScopeKindIDE)
	}
	if ev.Scope.Name != "infra" {
		t.Errorf("scope name = %q, want %q", ev.Scope.Name, "infra")
	}
}

func TestEventScope_JSONRoundTrip_WithSlash(t *testing.T) {
	scope := EventScope{Kind: "workspace", Name: "infra/feat"}

	data, err := json.Marshal(scope)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Should serialize as compact "kind:name" string
	if string(data) != `"workspace:infra/feat"` {
		t.Errorf("marshaled = %s, want %q", data, "workspace:infra/feat")
	}

	var decoded EventScope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != scope {
		t.Errorf("roundtrip: got %v, want %v", decoded, scope)
	}
}

func TestEventRecord_JSONSchema(t *testing.T) {
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra"},
		Event:     "clone_started",
		Timestamp: time.Date(2024, 6, 6, 14, 2, 20, 0, time.UTC),
		Detail:    "cloning repo",
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the JSON keys match the expected schema
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	requiredKeys := []string{"scope", "event", "timestamp"}
	for _, key := range requiredKeys {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing required JSON key %q", key)
		}
	}

	// Verify scope serializes as string, not object
	scopeVal, ok := raw["scope"].(string)
	if !ok {
		t.Errorf("scope should serialize as string, got %T", raw["scope"])
	}
	if scopeVal != "workspace:infra" {
		t.Errorf("scope = %q, want %q", scopeVal, "workspace:infra")
	}
}

func TestDaemonState_TopLevelSchema(t *testing.T) {
	state := DaemonState{
		Workspaces: map[string]*Workspace{
			"infra": {
				Spec: WorkspaceSpec{Name: "infra"},
			},
		},
		Credentials: map[string]*CredentialState{
			"jperez": {Owner: "jperez"},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Must have "workspaces" key (not "instances")
	if _, ok := raw["workspaces"]; !ok {
		t.Error("DaemonState missing 'workspaces' JSON key")
	}
	// Must not have legacy "instances" key
	if _, ok := raw["instances"]; ok {
		t.Error("DaemonState has legacy 'instances' JSON key")
	}
	// Must have "credentials" key
	if _, ok := raw["credentials"]; !ok {
		t.Error("DaemonState missing 'credentials' JSON key")
	}
}

func TestScopeKindConstants(t *testing.T) {
	// Verify the three aggregate scope kinds are defined
	cases := map[string]string{
		"ScopeKindWorkspace":   ScopeKindWorkspace,
		"ScopeKindIDE":         ScopeKindIDE,
		"ScopeKindCredentials": ScopeKindCredentials,
	}

	for name, val := range cases {
		if val == "" {
			t.Errorf("%s is empty", name)
		}
	}

	// Verify they are distinct
	if ScopeKindWorkspace == ScopeKindIDE || ScopeKindWorkspace == ScopeKindCredentials || ScopeKindIDE == ScopeKindCredentials {
		t.Error("scope kind constants must be distinct")
	}
}

func TestWorkspaceListItem_NoWorkspaceInstanceType(t *testing.T) {
	// Verify that list responses use WorkspaceListItem (not WorkspaceInstance)
	// by round-tripping through JSON and confirming the shape.
	ws := &Workspace{
		Spec:   WorkspaceSpec{Name: "infra"},
		Status: StatusReady,
	}

	item := WorkspaceListItemFromInstance(ws)
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Must have "spec" and "status" (Workspace fields)
	if _, ok := raw["spec"]; !ok {
		t.Error("WorkspaceListItem missing 'spec' key")
	}
	if _, ok := raw["status"]; !ok {
		t.Error("WorkspaceListItem missing 'status' key")
	}
}

func TestActivityLog_FormatAndParseRoundTrip(t *testing.T) {
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra"},
		Event:     "clone_started",
		Timestamp: time.Date(2024, 6, 6, 14, 2, 20, 0, time.UTC),
		Detail:    "cloning from github.com/org/repo",
	}

	line := formatLine(record)
	parsed, err := parseLine(line)
	if err != nil {
		t.Fatalf("parseLine: %v", err)
	}

	if parsed.Scope != record.Scope {
		t.Errorf("scope = %v, want %v", parsed.Scope, record.Scope)
	}
	if parsed.Event != record.Event {
		t.Errorf("event = %q, want %q", parsed.Event, record.Event)
	}
	if parsed.Detail != record.Detail {
		t.Errorf("detail = %q, want %q", parsed.Detail, record.Detail)
	}
}

func TestActivityLog_CrossAggregateEvents(t *testing.T) {
	// Verify that activity log can store and parse events from all three aggregates
	events := []EventRecord{
		{
			Scope:     EventScope{Kind: ScopeKindWorkspace, Name: "infra"},
			Event:     "clone_started",
			Timestamp: time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC),
		},
		{
			Scope:     EventScope{Kind: ScopeKindIDE, Name: "infra"},
			Event:     "ide_started",
			Timestamp: time.Date(2024, 6, 6, 14, 1, 0, 0, time.UTC),
		},
		{
			Scope:     EventScope{Kind: ScopeKindCredentials, Name: "jperez"},
			Event:     "git_credentials_written",
			Timestamp: time.Date(2024, 6, 6, 14, 2, 0, 0, time.UTC),
		},
	}

	for _, ev := range events {
		line := formatLine(ev)
		parsed, err := parseLine(line)
		if err != nil {
			t.Errorf("failed to parse %s event: %v", ev.Scope.Kind, err)
			continue
		}
		if parsed.Scope.Kind != ev.Scope.Kind {
			t.Errorf("scope kind = %q, want %q", parsed.Scope.Kind, ev.Scope.Kind)
		}
	}
}

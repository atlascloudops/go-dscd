package domain

import (
	"testing"
)

func TestCredentialState_RecordEvent_AppendsWithCorrectScope(t *testing.T) {
	cs := &CredentialState{Owner: "jperez"}

	cs.RecordEvent(CredEventGitWritten, "github.com, gitlab.com")

	if len(cs.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cs.Events))
	}

	evt := cs.Events[0]
	if evt.Scope.Kind != ScopeKindCredentials {
		t.Errorf("scope kind: expected %q, got %q", ScopeKindCredentials, evt.Scope.Kind)
	}
	if evt.Scope.Name != "jperez" {
		t.Errorf("scope name: expected %q, got %q", "jperez", evt.Scope.Name)
	}
	if evt.Scope.String() != "credentials:jperez" {
		t.Errorf("scope string: expected %q, got %q", "credentials:jperez", evt.Scope.String())
	}
	if evt.Event != string(CredEventGitWritten) {
		t.Errorf("event: expected %q, got %q", CredEventGitWritten, evt.Event)
	}
	if evt.Detail != "github.com, gitlab.com" {
		t.Errorf("detail: expected %q, got %q", "github.com, gitlab.com", evt.Detail)
	}
	if evt.Timestamp.IsZero() {
		t.Error("timestamp should not be zero")
	}
}

func TestCredentialState_RecordEvent_MultipleEvents(t *testing.T) {
	cs := &CredentialState{Owner: "admin"}

	cs.RecordEvent(CredEventGitWritten, "github.com")
	cs.RecordEvent(CredEventSsoTokenCached, "dsc-session")
	cs.RecordEvent(CredEventSsoConfigWritten, "3 profiles")

	if len(cs.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(cs.Events))
	}

	expected := []CredentialEvent{
		CredEventGitWritten,
		CredEventSsoTokenCached,
		CredEventSsoConfigWritten,
	}
	for i, want := range expected {
		if cs.Events[i].Event != string(want) {
			t.Errorf("event[%d]: expected %q, got %q", i, want, cs.Events[i].Event)
		}
		if cs.Events[i].Scope.Kind != ScopeKindCredentials {
			t.Errorf("event[%d] scope kind: expected %q, got %q", i, ScopeKindCredentials, cs.Events[i].Scope.Kind)
		}
		if cs.Events[i].Scope.Name != "admin" {
			t.Errorf("event[%d] scope name: expected %q, got %q", i, "admin", cs.Events[i].Scope.Name)
		}
	}
}

func TestCredentialState_RecordEvent_EmptyDetail(t *testing.T) {
	cs := &CredentialState{Owner: "testuser"}

	cs.RecordEvent(CredEventSsoTokenExpired, "")

	if len(cs.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cs.Events))
	}
	if cs.Events[0].Detail != "" {
		t.Errorf("detail: expected empty, got %q", cs.Events[0].Detail)
	}
}

func TestCredentialEvent_Constants(t *testing.T) {
	// Verify the string values of credential event constants
	cases := []struct {
		event CredentialEvent
		want  string
	}{
		{CredEventGitWritten, "git_credentials_written"},
		{CredEventGitRotated, "git_credentials_rotated"},
		{CredEventSsoTokenCached, "sso_token_cached"},
		{CredEventSsoConfigWritten, "sso_config_written"},
		{CredEventSsoTokenExpired, "sso_token_expired"},
	}
	for _, tc := range cases {
		if string(tc.event) != tc.want {
			t.Errorf("expected %q, got %q", tc.want, string(tc.event))
		}
	}
}

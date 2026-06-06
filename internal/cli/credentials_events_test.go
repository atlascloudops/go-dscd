package cli

import (
	"path/filepath"
	"testing"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/atlascloudops/go-dscd/internal/store"
)

func TestRecordGitCredentialEvent_WrittenEvent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	logPath := filepath.Join(dir, "activity.log")

	s := store.NewFileStore(statePath)
	al := domain.NewActivityLog(logPath)

	entries := []domain.GitCredentialEntry{
		{Host: "github.com", AuthUser: "user", Token: "tok1"},
		{Host: "gitlab.com", AuthUser: "user", Token: "tok2"},
	}

	// Both hosts are new (added), none updated
	added := []string{"github.com", "gitlab.com"}
	var updated []string

	recordGitCredentialEvent(s, al, "jperez", updated, added, entries)

	// Verify state was persisted
	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	cs := state.Credentials["jperez"]
	if cs == nil {
		t.Fatal("expected credential state for jperez")
	}

	if len(cs.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cs.Events))
	}

	evt := cs.Events[0]
	if evt.Event != string(domain.CredEventGitWritten) {
		t.Errorf("expected event %q, got %q", domain.CredEventGitWritten, evt.Event)
	}
	if evt.Scope.Kind != domain.ScopeKindCredentials {
		t.Errorf("expected scope kind %q, got %q", domain.ScopeKindCredentials, evt.Scope.Kind)
	}
	if evt.Scope.Name != "jperez" {
		t.Errorf("expected scope name %q, got %q", "jperez", evt.Scope.Name)
	}
	if evt.Detail != "github.com, gitlab.com" {
		t.Errorf("expected detail %q, got %q", "github.com, gitlab.com", evt.Detail)
	}

	// Verify activity log was appended
	records, err := al.Read(domain.ActivityLogFilter{})
	if err != nil {
		t.Fatalf("activity log Read: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 activity log record, got %d", len(records))
	}
	if records[0].Event != string(domain.CredEventGitWritten) {
		t.Errorf("activity log event: expected %q, got %q", domain.CredEventGitWritten, records[0].Event)
	}
	if records[0].Scope.String() != "credentials:jperez" {
		t.Errorf("activity log scope: expected %q, got %q", "credentials:jperez", records[0].Scope.String())
	}
}

func TestRecordGitCredentialEvent_RotatedEvent(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	logPath := filepath.Join(dir, "activity.log")

	s := store.NewFileStore(statePath)
	al := domain.NewActivityLog(logPath)

	entries := []domain.GitCredentialEntry{
		{Host: "github.com", AuthUser: "user", Token: "newtok"},
	}

	// All hosts updated, none added — should emit rotated event
	updated := []string{"github.com"}
	var added []string

	recordGitCredentialEvent(s, al, "jperez", updated, added, entries)

	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	cs := state.Credentials["jperez"]
	if cs == nil {
		t.Fatal("expected credential state for jperez")
	}

	if len(cs.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cs.Events))
	}

	if cs.Events[0].Event != string(domain.CredEventGitRotated) {
		t.Errorf("expected event %q, got %q", domain.CredEventGitRotated, cs.Events[0].Event)
	}

	// Verify activity log
	records, err := al.Read(domain.ActivityLogFilter{})
	if err != nil {
		t.Fatalf("activity log Read: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 activity log record, got %d", len(records))
	}
	if records[0].Event != string(domain.CredEventGitRotated) {
		t.Errorf("activity log event: expected %q, got %q", domain.CredEventGitRotated, records[0].Event)
	}
}

func TestRecordGitCredentialEvent_NilActivityLog(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	s := store.NewFileStore(statePath)

	entries := []domain.GitCredentialEntry{
		{Host: "github.com", AuthUser: "user", Token: "tok"},
	}

	// Should not panic with nil activity log
	recordGitCredentialEvent(s, nil, "jperez", nil, []string{"github.com"}, entries)

	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	cs := state.Credentials["jperez"]
	if cs == nil {
		t.Fatal("expected credential state for jperez")
	}
	if len(cs.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cs.Events))
	}
}

func TestRecordSsoCredentialEvents_EmitsBothEvents(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	logPath := filepath.Join(dir, "activity.log")

	s := store.NewFileStore(statePath)
	al := domain.NewActivityLog(logPath)

	payload := domain.SsoWritePayload{
		Session: domain.SsoSessionEntry{
			SessionName:           "dsc",
			SsoStartUrl:           "https://start.example.com",
			SsoRegion:             "us-east-1",
			SsoRegistrationScopes: "sso:account:access",
		},
		Profiles: []domain.AwsProfileEntry{
			{Name: "dev", AccountId: "111111111111", RoleName: "Developer"},
			{Name: "staging", AccountId: "222222222222", RoleName: "Developer"},
			{Name: "prod", AccountId: "333333333333", RoleName: "ReadOnly"},
		},
		Token: domain.SsoTokenEntry{
			AccessToken: "test-token",
			ExpiresAt:   "2024-12-31T23:59:59Z",
		},
	}

	recordSsoCredentialEvents(s, al, "jperez", payload)

	// Verify state
	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	cs := state.Credentials["jperez"]
	if cs == nil {
		t.Fatal("expected credential state for jperez")
	}

	if len(cs.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(cs.Events))
	}

	// First event: sso_config_written
	if cs.Events[0].Event != string(domain.CredEventSsoConfigWritten) {
		t.Errorf("event[0]: expected %q, got %q", domain.CredEventSsoConfigWritten, cs.Events[0].Event)
	}
	if cs.Events[0].Detail != "session=dsc, profiles=3" {
		t.Errorf("event[0] detail: expected %q, got %q", "session=dsc, profiles=3", cs.Events[0].Detail)
	}

	// Second event: sso_token_cached
	if cs.Events[1].Event != string(domain.CredEventSsoTokenCached) {
		t.Errorf("event[1]: expected %q, got %q", domain.CredEventSsoTokenCached, cs.Events[1].Event)
	}
	if cs.Events[1].Detail != "dsc" {
		t.Errorf("event[1] detail: expected %q, got %q", "dsc", cs.Events[1].Detail)
	}

	// Verify read projections
	if cs.SsoSession != "dsc" {
		t.Errorf("SsoSession: expected %q, got %q", "dsc", cs.SsoSession)
	}
	if cs.LastSyncedAt == nil {
		t.Error("LastSyncedAt should be set")
	}

	// Verify activity log has both events
	records, err := al.Read(domain.ActivityLogFilter{})
	if err != nil {
		t.Fatalf("activity log Read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 activity log records, got %d", len(records))
	}

	if records[0].Event != string(domain.CredEventSsoConfigWritten) {
		t.Errorf("activity log[0]: expected %q, got %q", domain.CredEventSsoConfigWritten, records[0].Event)
	}
	if records[0].Scope.String() != "credentials:jperez" {
		t.Errorf("activity log[0] scope: expected %q, got %q", "credentials:jperez", records[0].Scope.String())
	}

	if records[1].Event != string(domain.CredEventSsoTokenCached) {
		t.Errorf("activity log[1]: expected %q, got %q", domain.CredEventSsoTokenCached, records[1].Event)
	}
}

func TestRecordSsoCredentialEvents_NilActivityLog(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	s := store.NewFileStore(statePath)

	payload := domain.SsoWritePayload{
		Session: domain.SsoSessionEntry{
			SessionName:           "dsc",
			SsoStartUrl:           "https://start.example.com",
			SsoRegion:             "us-east-1",
			SsoRegistrationScopes: "sso:account:access",
		},
		Profiles: []domain.AwsProfileEntry{
			{Name: "dev", AccountId: "111111111111", RoleName: "Developer"},
		},
		Token: domain.SsoTokenEntry{
			AccessToken: "test-token",
			ExpiresAt:   "2024-12-31T23:59:59Z",
		},
	}

	// Should not panic with nil activity log
	recordSsoCredentialEvents(s, nil, "jperez", payload)

	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	cs := state.Credentials["jperez"]
	if cs == nil {
		t.Fatal("expected credential state for jperez")
	}
	if len(cs.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(cs.Events))
	}
}

func TestRecordGitCredentialEvent_UpdatesReadProjections(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	logPath := filepath.Join(dir, "activity.log")

	s := store.NewFileStore(statePath)
	al := domain.NewActivityLog(logPath)

	entries := []domain.GitCredentialEntry{
		{Host: "github.com", AuthUser: "user", Token: "tok"},
		{Host: "gitlab.com", AuthUser: "user", Token: "tok2"},
	}

	recordGitCredentialEvent(s, al, "admin", nil, []string{"github.com", "gitlab.com"}, entries)

	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	cs := state.Credentials["admin"]
	if cs == nil {
		t.Fatal("expected credential state for admin")
	}

	if len(cs.GitHosts) != 2 {
		t.Fatalf("expected 2 git hosts, got %d", len(cs.GitHosts))
	}
	if cs.GitHosts[0] != "github.com" || cs.GitHosts[1] != "gitlab.com" {
		t.Errorf("unexpected git hosts: %v", cs.GitHosts)
	}
	if cs.LastSyncedAt == nil {
		t.Error("LastSyncedAt should be set")
	}
}

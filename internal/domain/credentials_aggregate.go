package domain

import "time"

// CredentialEvent is a typed string constant representing a credential lifecycle event.
type CredentialEvent string

const (
	CredEventGitWritten      CredentialEvent = "git_credentials_written"
	CredEventGitRotated      CredentialEvent = "git_credentials_rotated"
	CredEventSsoTokenCached  CredentialEvent = "sso_token_cached"
	CredEventSsoConfigWritten CredentialEvent = "sso_config_written"
	CredEventSsoTokenExpired CredentialEvent = "sso_token_expired"
)

// CredentialState is the aggregate root for credential operations scoped to a
// Linux user (owner). It maintains an event stream and read projections for
// git credential hosts and SSO session state.
type CredentialState struct {
	Owner        string        `json:"owner"`
	Events       []EventRecord `json:"events,omitempty"`
	GitHosts     []string      `json:"git_hosts,omitempty"`
	SsoSession   string        `json:"sso_session,omitempty"`
	LastSyncedAt *time.Time    `json:"last_synced_at,omitempty"`
}

// RecordEvent appends an EventRecord to the credential event stream with the
// scope set to "credentials:<owner>".
func (cs *CredentialState) RecordEvent(event CredentialEvent, detail string) {
	scope, _ := NewEventScope(ScopeKindCredentials, cs.Owner)
	cs.Events = append(cs.Events, EventRecord{
		Scope:     scope,
		Event:     string(event),
		Timestamp: time.Now().UTC(),
		Detail:    detail,
	})
}

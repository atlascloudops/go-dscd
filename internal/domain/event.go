package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Predefined scope kind constants.
const (
	ScopeKindWorkspace   = "workspace"
	ScopeKindIDE         = "ide"
	ScopeKindCredentials = "credentials"
)

// EventScope is a value object that identifies the aggregate instance an event
// belongs to. It encodes as the compact "kind:name" string form in JSON.
type EventScope struct {
	Kind string // "workspace", "ide", "credentials"
	Name string // "infra", "infra/feat", "jperez"
}

// NewEventScope creates an EventScope after validating kind and name.
// Kind must be non-empty, lowercase alphanumeric (no colons, no slashes).
// Name must be non-empty.
func NewEventScope(kind, name string) (EventScope, error) {
	if kind == "" {
		return EventScope{}, fmt.Errorf("event scope kind must not be empty")
	}
	for _, r := range kind {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return EventScope{}, fmt.Errorf("event scope kind %q contains invalid character %q: must be lowercase alphanumeric", kind, string(r))
		}
	}
	if name == "" {
		return EventScope{}, fmt.Errorf("event scope name must not be empty")
	}
	return EventScope{Kind: kind, Name: name}, nil
}

// String returns the canonical "kind:name" representation.
func (s EventScope) String() string {
	return s.Kind + ":" + s.Name
}

// MarshalJSON serializes EventScope as a JSON string in "kind:name" format.
func (s EventScope) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON deserializes EventScope from a JSON "kind:name" string.
func (s *EventScope) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("event scope: expected JSON string: %w", err)
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("event scope: invalid format %q, expected \"kind:name\"", raw)
	}
	s.Kind = parts[0]
	s.Name = parts[1]
	return nil
}

// EventRecord is a single immutable event entry in a unified event stream.
// All aggregates (workspace, IDE, credentials) emit EventRecord values.
type EventRecord struct {
	Scope     EventScope `json:"scope"`
	Event     string     `json:"event"`
	Timestamp time.Time  `json:"timestamp"`
	Detail    string     `json:"detail,omitempty"`
}

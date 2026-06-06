package domain

import (
	"encoding/json"
	"time"
)

// WorkspaceSpec is the input definition — what the client asks for.
type WorkspaceSpec struct {
	Name          string    `json:"name"`           // user-facing alias: "infra" or custom name
	CanonicalName string    `json:"canonical_name"` // VCS-derived identity: "infra" or "infra/feat"
	VCS           VCSTarget `json:"vcs"`
	PatName       string    `json:"pat_name"`
	ProjectRoot  string    `json:"project_root"`  // final worktree path
	RepoRoot     string    `json:"repo_root"`     // container dir (holds .bare/ and worktrees)
	BareRoot     string    `json:"bare_root"`     // path to .bare/
	WorktreeName string    `json:"worktree_name"` // "default" or branch-derived
	IsDefault    bool      `json:"is_default"`    // true = bare clone + first worktree
	Owner        string          `json:"owner"`
	IDE          *IDESpecConfig  `json:"ide,omitempty"`
	Template     *TemplateSource `json:"template,omitempty"`
}

// TemplateSource describes the template repository used to seed a workspace.
type TemplateSource struct {
	CloneURL string `json:"clone_url"`
	Host     string `json:"host"`
	Repo     string `json:"repo"`
}

// IDESpecConfig is the optional IDE configuration on a workspace spec.
// When set, provisioning will start an IDE adapter after worktree creation.
type IDESpecConfig struct {
	Adapter string `json:"adapter"` // e.g. "openvscode-server"
}

type VCSTarget struct {
	Host     string `json:"host"`
	AuthUser string `json:"auth_user"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	CloneURL string `json:"clone_url,omitempty"`
}

// Workspace is the realized state — what actually exists on the pod.
// Formerly WorkspaceInstance; promoted to a proper aggregate with RecordEvent.
type Workspace struct {
	Spec          WorkspaceSpec `json:"spec"`
	Events        []EventRecord `json:"events,omitempty"`
	Status        Status        `json:"status,omitempty"`
	IDE           *IDEInstance   `json:"ide,omitempty"`
	HeadCommit    string        `json:"head_commit,omitempty"`
	ProvisionedAt *time.Time    `json:"provisioned_at,omitempty"`
	LastError     *string       `json:"last_error,omitempty"`
	LastSyncedAt  *time.Time    `json:"last_synced_at,omitempty"`
}

// RecordEvent is the sole entry point for workspace event emission. It constructs
// an EventRecord with the workspace's scope, appends it to the event stream, and
// re-projects the workspace status.
func (w *Workspace) RecordEvent(event WorkspaceEvent, detail string) {
	scope := EventScope{Kind: ScopeKindWorkspace, Name: w.Spec.Name}
	w.Events = append(w.Events, EventRecord{
		Scope:     scope,
		Event:     string(event),
		Timestamp: time.Now().UTC(),
		Detail:    detail,
	})
	var resolver WorkspaceStatusResolver
	w.Status = resolver.Resolve(w.Events)
}

// UnmarshalJSON handles backward-compatible deserialization of Workspace.
// Old state files store events as WorkspaceEventRecord (no scope); new ones use
// EventRecord (with scope). This method detects which format is present and
// converts old-format events into EventRecord with a workspace scope derived
// from the spec name.
func (w *Workspace) UnmarshalJSON(data []byte) error {
	// Alias avoids infinite recursion on UnmarshalJSON.
	type Alias Workspace
	type workspaceWithRawEvents struct {
		Alias
		RawEvents []json.RawMessage `json:"events,omitempty"`
	}
	var raw workspaceWithRawEvents
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*w = Workspace(raw.Alias)
	w.Events = nil

	for _, re := range raw.RawEvents {
		// Try new EventRecord format first (has "scope" key).
		var er EventRecord
		if err := json.Unmarshal(re, &er); err == nil && er.Scope.Kind != "" {
			w.Events = append(w.Events, er)
			continue
		}
		// Fall back to old WorkspaceEventRecord format.
		var old WorkspaceEventRecord
		if err := json.Unmarshal(re, &old); err == nil && old.Event != "" {
			w.Events = append(w.Events, old.ToEventRecord(w.Spec.Name))
			continue
		}
	}
	return nil
}

// DisplayStatus returns a human-readable status string derived from Status.
func (w *Workspace) DisplayStatus() string {
	switch w.Status {
	case StatusFailed:
		return "ERROR"
	case StatusReady:
		return "SYNCED"
	default:
		return "MISSING"
	}
}

// PruneResult holds the outcome of a prune operation.
type PruneResult struct {
	Pruned  []string       `json:"pruned"`  // workspace names removed
	Skipped []PruneSkipped `json:"skipped"` // workspace names kept, with reason
	Message string         `json:"message"`
}

// PruneSkipped describes a worktree that was not pruned, and why.
type PruneSkipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"` // e.g. "uncommitted changes", "default worktree"
}


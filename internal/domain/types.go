package domain

import "time"

// WorkspaceSpec is the input definition — what the client asks for.
type WorkspaceSpec struct {
	Name         string    `json:"name"`          // logical name: "infra" or "infra/feature-vpc"
	VCS          VCSTarget `json:"vcs"`
	PatName      string    `json:"pat_name"`
	ProjectRoot  string    `json:"project_root"`  // final worktree path
	RepoRoot     string    `json:"repo_root"`     // container dir (holds .bare/ and worktrees)
	BareRoot     string    `json:"bare_root"`     // path to .bare/
	WorktreeName string    `json:"worktree_name"` // "default" or branch-derived
	IsDefault    bool      `json:"is_default"`    // true = bare clone + first worktree
	Owner        string         `json:"owner"`
	IDE          *IDESpecConfig `json:"ide,omitempty"`
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
	CloneURL string `json:"clone_url"`
}

// WorkspaceInstance is the realized state — what actually exists on the pod.
type WorkspaceInstance struct {
	Spec            WorkspaceSpec          `json:"spec"`
	Events          []WorkspaceEventRecord `json:"events,omitempty"`
	Lifecycle       LifecycleStatus        `json:"lifecycle,omitempty"`
	IDE             *IDEState              `json:"ide,omitempty"`
	HeadCommit      string                 `json:"head_commit,omitempty"`
	CredentialHost  string                 `json:"credential_host"`
	ProvisionedAt   *time.Time             `json:"provisioned_at,omitempty"`
	LastError       *string                `json:"last_error,omitempty"`
	LastSyncedAt    *time.Time             `json:"last_synced_at,omitempty"`
	CredentialFresh bool                   `json:"-"`
}

// appendEvent appends an event record and keeps Lifecycle in sync.
func appendEvent(inst *WorkspaceInstance, event WorkspaceEvent, detail string) {
	inst.Events = append(inst.Events, WorkspaceEventRecord{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Detail:    detail,
	})
	inst.Lifecycle = ResolveLifecycleStatus(inst.Events)
}

// DisplayStatus returns a human-readable status string derived from Lifecycle.
func (w *WorkspaceInstance) DisplayStatus() string {
	switch w.Lifecycle {
	case LifecycleFailed:
		return "ERROR"
	case LifecycleReady:
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


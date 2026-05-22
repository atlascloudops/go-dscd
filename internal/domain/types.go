package domain

import "time"

type WorkspaceState string

const (
	StatePending      WorkspaceState = "pending"
	StateProvisioning WorkspaceState = "provisioning"
	StateReady        WorkspaceState = "ready"
	StateError        WorkspaceState = "error"
)

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
	Owner        string    `json:"owner"`
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
	State           WorkspaceState         `json:"state"`
	Status          string                 `json:"status"`
	Events          []WorkspaceEventRecord `json:"events,omitempty"`
	Lifecycle       LifecycleStatus        `json:"lifecycle,omitempty"`
	HeadCommit      string                 `json:"head_commit,omitempty"`
	CredentialHost  string                 `json:"credential_host"`
	ProvisionedAt   *time.Time             `json:"provisioned_at,omitempty"`
	LastError       *string                `json:"last_error,omitempty"`
	LastSyncedAt    *time.Time             `json:"last_synced_at,omitempty"`   // renamed from last_reconcile_at
	// Internal fields — used for status derivation, excluded from JSON output.
	CloneExists     bool `json:"-"`
	CredentialFresh bool `json:"-"`
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

// DeriveStatus sets Status from CloneExists and State.
func (w *WorkspaceInstance) DeriveStatus() {
	switch {
	case w.State == StateError:
		w.Status = "ERROR"
	case w.CloneExists:
		w.Status = "SYNCED"
	default:
		w.Status = "MISSING"
	}
}

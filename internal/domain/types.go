package domain

import "time"

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

// WorkspaceInstance is the realized state — what actually exists on the pod.
type WorkspaceInstance struct {
	Spec            WorkspaceSpec          `json:"spec"`
	Events          []WorkspaceEventRecord `json:"events,omitempty"`
	Status          Status                 `json:"status,omitempty"`
	IDE             *IDEInstance            `json:"ide,omitempty"`
	HeadCommit      string                 `json:"head_commit,omitempty"`
	CredentialHost  string                 `json:"credential_host"`
	ProvisionedAt   *time.Time             `json:"provisioned_at,omitempty"`
	LastError       *string                `json:"last_error,omitempty"`
	LastSyncedAt    *time.Time             `json:"last_synced_at,omitempty"`
}

// appendEvent appends a workspace event record and keeps Status in sync.
func appendEvent(inst *WorkspaceInstance, event WorkspaceEvent, detail string) {
	inst.Events = append(inst.Events, WorkspaceEventRecord{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Detail:    detail,
	})
	var resolver WorkspaceStatusResolver
	inst.Status = resolver.Resolve(inst.Events)
}

// appendIDEEvent appends an IDE event record to an IDEInstance and re-projects
// its Status via IDEStatusResolver.
func appendIDEEvent(ide *IDEInstance, event IDEEvent, detail string) {
	ide.Events = append(ide.Events, IDEEventRecord{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Detail:    detail,
	})
	var resolver IDEStatusResolver
	ide.Status = resolver.Resolve(ide.Events)
}

// DisplayStatus returns a human-readable status string derived from Status.
func (w *WorkspaceInstance) DisplayStatus() string {
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


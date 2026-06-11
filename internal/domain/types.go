package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// RepoInfo is a child entity capturing the VCS identity of the workspace's repository.
type RepoInfo struct {
	Host     string `json:"host"`      // e.g. "github.com"
	Slug     string `json:"slug"`      // e.g. "org/repo"
	CloneURL string `json:"clone_url"` // e.g. "https://github.com/org/repo.git"
}

// Worktree is a child entity within a Workspace, representing a single worktree
// checked out from the bare clone.
type Worktree struct {
	Name        string `json:"name"`
	Branch      string `json:"branch"`
	ProjectRoot string `json:"project_root"`
	IsDefault   bool   `json:"is_default"`
	HeadCommit  string `json:"head_commit,omitempty"`
}

// WorkspaceSpec is the provision-time input DTO — identity and intent only, no paths.
// Path fields are server-derived and live on the Workspace aggregate.
// Worktree fields live on the Worktree child entity.
type WorkspaceSpec struct {
	Name     string          `json:"name"`
	VCS      VCSTarget       `json:"vcs"`
	PatName  string          `json:"pat_name"`
	Owner    string          `json:"owner"`
	Template *TemplateSource `json:"template,omitempty"`
}

// TemplateSource describes the template repository used to seed a workspace.
type TemplateSource struct {
	CloneURL string `json:"clone_url"`
	Host     string `json:"host"`
	Repo     string `json:"repo"`
}

// VCSTarget identifies the repository to clone. Simplified to match RepoInfo fields:
// Host, Repo, CloneURL only. Branch and AuthUser have been removed.
type VCSTarget struct {
	Host     string `json:"host"`
	Repo     string `json:"repo"`
	CloneURL string `json:"clone_url,omitempty"`
}

// ProvisionParams bundles a WorkspaceSpec with the server-owned workspace root.
// All filesystem paths (RepoRoot, BareRoot, ProjectRoot) are derived server-side
// from the workspace root and the spec's VCS identity. Clients never send paths.
type ProvisionParams struct {
	Spec          WorkspaceSpec `json:"spec"`
	WorkspaceRoot string        `json:"-"` // resolved at CLI startup, not serialized
}

// RepoRoot derives the repo container directory from the provision params.
func (p ProvisionParams) RepoRoot() string {
	spec := p.Spec
	if spec.Template != nil && spec.VCS.CloneURL == "" {
		return DeriveLocalRepoRoot(p.WorkspaceRoot, spec.Name)
	}
	return DeriveRepoRoot(p.WorkspaceRoot, spec.VCS.Host, spec.VCS.Repo)
}

// BareRoot derives the bare clone directory from the provision params.
func (p ProvisionParams) BareRoot() string {
	return DeriveBareRoot(p.RepoRoot())
}

// ProjectRoot derives the worktree checkout directory from the spec name.
// If the spec name contains "/", the second segment is the worktree name
// (e.g. "myrepo/feature" -> worktree "feature"). Otherwise, "default".
func (p ProvisionParams) ProjectRoot() string {
	worktreeName := "default"
	if parts := strings.SplitN(p.Spec.Name, "/", 2); len(parts) == 2 {
		worktreeName = parts[1]
	}
	return DeriveProjectRoot(p.RepoRoot(), worktreeName)
}

// Workspace is the aggregate root — one per repo per pod.
// It represents a bare clone container with child worktree entities.
type Workspace struct {
	Name          string                 `json:"name"`
	Repo          RepoInfo               `json:"repo"`
	RepoRoot      string                 `json:"repo_root"`
	BareRoot      string                 `json:"bare_root"`
	Owner         string                 `json:"owner"`
	PatName       string                 `json:"pat_name"`
	Template      *TemplateSource        `json:"template,omitempty"`
	Worktrees     []Worktree             `json:"worktrees"`
	Events        []EventRecord          `json:"events,omitempty"`
	Status        Status                 `json:"status,omitempty"`
	IDE           map[string]*IDEInstance `json:"ide,omitempty"`
	ProvisionedAt *time.Time             `json:"provisioned_at,omitempty"`
	LastError     *string                `json:"last_error,omitempty"`
	LastSyncedAt  *time.Time             `json:"last_synced_at,omitempty"`
}

// DefaultWorktree returns the first worktree with IsDefault=true, or nil if none.
func (w *Workspace) DefaultWorktree() *Worktree {
	for i := range w.Worktrees {
		if w.Worktrees[i].IsDefault {
			return &w.Worktrees[i]
		}
	}
	return nil
}

// FindWorktree returns the worktree with the given name, or nil if not found.
func (w *Workspace) FindWorktree(name string) *Worktree {
	for i := range w.Worktrees {
		if w.Worktrees[i].Name == name {
			return &w.Worktrees[i]
		}
	}
	return nil
}

// FindWorktreeByBranch returns the worktree with the given branch name, or nil if not found.
func (w *Workspace) FindWorktreeByBranch(branch string) *Worktree {
	for i := range w.Worktrees {
		if w.Worktrees[i].Branch == branch {
			return &w.Worktrees[i]
		}
	}
	return nil
}

// RemoveWorktreeByName removes the worktree with the given name from the Worktrees slice.
// Returns true if found and removed, false if not found.
func (w *Workspace) RemoveWorktreeByName(name string) bool {
	for i := range w.Worktrees {
		if w.Worktrees[i].Name == name {
			w.Worktrees = append(w.Worktrees[:i], w.Worktrees[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveIDEForWorktree deletes the IDE entry for the given worktree name.
func (w *Workspace) RemoveIDEForWorktree(worktreeName string) {
	if w.IDE != nil {
		delete(w.IDE, worktreeName)
	}
}

// DefaultProjectRoot returns the ProjectRoot of the default worktree, or empty string.
func (w *Workspace) DefaultProjectRoot() string {
	if wt := w.DefaultWorktree(); wt != nil {
		return wt.ProjectRoot
	}
	return ""
}

// IDEForWorktree returns the IDEInstance for the given worktree name, or nil.
func (w *Workspace) IDEForWorktree(worktreeName string) *IDEInstance {
	if w.IDE == nil {
		return nil
	}
	return w.IDE[worktreeName]
}

// SetIDEForWorktree sets the IDEInstance for the given worktree name.
func (w *Workspace) SetIDEForWorktree(worktreeName string, ide *IDEInstance) {
	if w.IDE == nil {
		w.IDE = make(map[string]*IDEInstance)
	}
	w.IDE[worktreeName] = ide
}

// RecordEvent is the sole entry point for workspace event emission. It constructs
// an EventRecord with the workspace's scope, appends it to the event stream, and
// re-projects the workspace status.
func (w *Workspace) RecordEvent(event WorkspaceEvent, detail string) {
	scope := EventScope{Kind: ScopeKindWorkspace, Name: w.Name}
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
// from the workspace name.
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
			w.Events = append(w.Events, old.ToEventRecord(w.Name))
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
	Pruned  []string       `json:"pruned"`  // worktree names removed
	Skipped []PruneSkipped `json:"skipped"` // worktree names kept, with reason
	Message string         `json:"message"`
}

// PruneSkipped describes a worktree that was not pruned, and why.
type PruneSkipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"` // e.g. "uncommitted changes", "default worktree"
}

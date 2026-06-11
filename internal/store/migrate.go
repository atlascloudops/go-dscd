package store

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
)

const (
	// StateVersionV1 is the original worktree-centric state format.
	StateVersionV1 = "v1"
	// StateVersionV2 is the container-centric aggregate format.
	StateVersionV2 = "v2"
)

// legacyWorkspace mirrors the old v1 Workspace structure for deserialization.
// In v1, each state entry was one worktree with a flat Spec containing both
// repo-level and worktree-level fields, and IDE was a single *IDEInstance.
type legacyWorkspace struct {
	Spec          legacyWorkspaceSpec `json:"spec"`
	Events        []json.RawMessage   `json:"events,omitempty"`
	Status        domain.Status       `json:"status,omitempty"`
	IDE           *domain.IDEInstance  `json:"ide,omitempty"`
	HeadCommit    string              `json:"head_commit,omitempty"`
	ProvisionedAt *time.Time          `json:"provisioned_at,omitempty"`
	LastError     *string             `json:"last_error,omitempty"`
	LastSyncedAt  *time.Time          `json:"last_synced_at,omitempty"`
}

// legacyWorkspaceSpec mirrors the old v1 WorkspaceSpec with all worktree-level
// fields that have since been moved to the Worktree child entity.
type legacyWorkspaceSpec struct {
	Name          string               `json:"name"`
	CanonicalName string               `json:"canonical_name"`
	VCS           legacyVCSTarget      `json:"vcs"`
	PatName       string               `json:"pat_name"`
	ProjectRoot   string               `json:"project_root"`
	RepoRoot      string               `json:"repo_root"`
	BareRoot      string               `json:"bare_root"`
	WorktreeName  string               `json:"worktree_name"`
	IsDefault     bool                 `json:"is_default"`
	Owner         string               `json:"owner"`
	IDE           *legacyIDESpecConfig `json:"ide,omitempty"`
	Template      *domain.TemplateSource `json:"template,omitempty"`
}

// legacyVCSTarget mirrors the old VCSTarget with Branch and AuthUser fields.
type legacyVCSTarget struct {
	Host     string `json:"host"`
	AuthUser string `json:"auth_user"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	CloneURL string `json:"clone_url,omitempty"`
}

// legacyIDESpecConfig mirrors the old IDESpecConfig.
type legacyIDESpecConfig struct {
	Adapter string `json:"adapter"`
}

// legacyEntry pairs a state map key with its deserialized legacy workspace.
type legacyEntry struct {
	key       string
	workspace legacyWorkspace
}

// legacyStateFile is used to deserialize a v1 state file where workspace values
// may be in the old format.
type legacyStateFile struct {
	Version     string                            `json:"version"`
	UpdatedAt   time.Time                         `json:"updated_at"`
	Workspaces  map[string]json.RawMessage        `json:"workspaces"`
	Credentials map[string]*domain.CredentialState `json:"credentials,omitempty"`
}

// needsMigrationRaw inspects raw JSON bytes to determine if a state file uses
// the old v1 worktree-centric format. This avoids deserializing through the new
// Workspace type which would lose old-format markers like spec.worktree_name.
func needsMigrationRaw(data []byte) bool {
	var envelope struct {
		Version    string                         `json:"version"`
		Workspaces map[string]json.RawMessage     `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false
	}
	if envelope.Version == StateVersionV2 {
		return false
	}

	for key, raw := range envelope.Workspaces {
		// Slash in key means old worktree-centric keying (e.g. "infra/feat").
		if strings.Contains(key, "/") {
			return true
		}

		// Probe workspace JSON for old-format markers.
		var probe struct {
			Spec *struct {
				WorktreeName string `json:"worktree_name"`
			} `json:"spec"`
			Worktrees []json.RawMessage `json:"worktrees"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		// Old format: has a "spec" object with "worktree_name".
		if probe.Spec != nil && probe.Spec.WorktreeName != "" {
			return true
		}
		// Old format: no "worktrees" array but has a "spec" object.
		if probe.Worktrees == nil && probe.Spec != nil {
			return true
		}
	}
	return false
}

// migrateV1ToV2 converts a v1 state file (worktree-centric) to v2 (container-centric).
// It re-reads the raw JSON to deserialize using legacy types, groups entries by RepoRoot,
// and builds new Workspace aggregates.
func migrateV1ToV2(data []byte) (*StateFile, error) {
	var legacy legacyStateFile
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, err
	}

	// Deserialize each workspace entry using legacy types.
	var entries []legacyEntry
	for key, raw := range legacy.Workspaces {
		var lw legacyWorkspace
		if err := json.Unmarshal(raw, &lw); err != nil {
			slog.Warn("skipping corrupt workspace entry during migration", "key", key, "error", err)
			continue
		}
		entries = append(entries, legacyEntry{key: key, workspace: lw})
	}

	// Group entries by RepoRoot to reconstruct container-level aggregates.
	groups := make(map[string][]legacyEntry)
	for _, entry := range entries {
		repoRoot := entry.workspace.Spec.RepoRoot
		if repoRoot == "" {
			slog.Warn("skipping workspace entry with empty RepoRoot during migration", "key", entry.key)
			continue
		}
		groups[repoRoot] = append(groups[repoRoot], entry)
	}

	// Build new Workspace aggregates from groups.
	newWorkspaces := make(map[string]*domain.Workspace)
	for _, group := range groups {
		ws := buildWorkspaceFromGroup(group)
		if ws != nil {
			newWorkspaces[ws.Name] = ws
		}
	}

	return &StateFile{
		Version:     StateVersionV2,
		UpdatedAt:   legacy.UpdatedAt,
		Workspaces:  newWorkspaces,
		Credentials: legacy.Credentials,
	}, nil
}

// buildWorkspaceFromGroup constructs a single v2 Workspace aggregate from a group
// of legacy entries that share the same RepoRoot.
func buildWorkspaceFromGroup(group []legacyEntry) *domain.Workspace {
	if len(group) == 0 {
		return nil
	}

	// Find the default entry to use for repo-level fields.
	// Prefer the entry with IsDefault=true; fall back to the first entry.
	var defaultEntry *legacyEntry
	for i := range group {
		if group[i].workspace.Spec.IsDefault {
			defaultEntry = &group[i]
			break
		}
	}
	if defaultEntry == nil {
		defaultEntry = &group[0]
	}

	spec := defaultEntry.workspace.Spec

	// Build RepoInfo from the spec's VCS fields.
	repo := domain.RepoInfo{
		Host:     spec.VCS.Host,
		Slug:     spec.VCS.Repo,
		CloneURL: spec.VCS.CloneURL,
	}

	// Build Worktrees slice from all entries in the group.
	// Track seen worktree names to handle duplicates (keep most recent).
	type worktreeCandidate struct {
		worktree  domain.Worktree
		latestEvt time.Time
	}
	seen := make(map[string]worktreeCandidate)

	for _, entry := range group {
		es := entry.workspace.Spec
		wtName := es.WorktreeName
		if wtName == "" {
			wtName = "default"
		}

		branch := es.VCS.Branch
		if branch == "" {
			branch = "main"
		}

		wt := domain.Worktree{
			Name:        wtName,
			Branch:      branch,
			ProjectRoot: es.ProjectRoot,
			IsDefault:   es.IsDefault,
			HeadCommit:  entry.workspace.HeadCommit,
		}

		// Determine the latest event timestamp for duplicate resolution.
		var latestEvt time.Time
		for _, rawEvt := range entry.workspace.Events {
			var ts struct {
				Timestamp time.Time `json:"timestamp"`
			}
			if err := json.Unmarshal(rawEvt, &ts); err == nil && ts.Timestamp.After(latestEvt) {
				latestEvt = ts.Timestamp
			}
		}

		if existing, ok := seen[wtName]; ok {
			// Keep the entry with the most recent event.
			if latestEvt.After(existing.latestEvt) {
				seen[wtName] = worktreeCandidate{worktree: wt, latestEvt: latestEvt}
			}
		} else {
			seen[wtName] = worktreeCandidate{worktree: wt, latestEvt: latestEvt}
		}
	}

	worktrees := make([]domain.Worktree, 0, len(seen))
	for _, candidate := range seen {
		worktrees = append(worktrees, candidate.worktree)
	}
	// Sort worktrees: default first, then alphabetical by name.
	sort.Slice(worktrees, func(i, j int) bool {
		if worktrees[i].IsDefault != worktrees[j].IsDefault {
			return worktrees[i].IsDefault
		}
		return worktrees[i].Name < worktrees[j].Name
	})

	// Merge events from all entries, sorted by timestamp.
	var allEvents []domain.EventRecord
	for _, entry := range group {
		for _, rawEvt := range entry.workspace.Events {
			// Try new EventRecord format first (has "scope" key).
			var er domain.EventRecord
			if err := json.Unmarshal(rawEvt, &er); err == nil && er.Scope.Kind != "" {
				allEvents = append(allEvents, er)
				continue
			}
			// Fall back to old WorkspaceEventRecord format.
			var old domain.WorkspaceEventRecord
			if err := json.Unmarshal(rawEvt, &old); err == nil && old.Event != "" {
				allEvents = append(allEvents, old.ToEventRecord(spec.Name))
				continue
			}
		}
	}
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Timestamp.Before(allEvents[j].Timestamp)
	})

	// Build IDE map from entries that had an IDEInstance.
	var ideMap map[string]*domain.IDEInstance
	for _, entry := range group {
		if entry.workspace.IDE != nil {
			if ideMap == nil {
				ideMap = make(map[string]*domain.IDEInstance)
			}
			// Key by worktree name.
			wtName := entry.workspace.Spec.WorktreeName
			if wtName == "" {
				wtName = "default"
			}
			ideMap[wtName] = entry.workspace.IDE
		}
	}

	// Find the earliest ProvisionedAt across the group.
	var earliestProvisioned *time.Time
	for _, entry := range group {
		if entry.workspace.ProvisionedAt != nil {
			if earliestProvisioned == nil || entry.workspace.ProvisionedAt.Before(*earliestProvisioned) {
				t := *entry.workspace.ProvisionedAt
				earliestProvisioned = &t
			}
		}
	}

	// Find the most recent LastSyncedAt.
	var latestSynced *time.Time
	for _, entry := range group {
		if entry.workspace.LastSyncedAt != nil {
			if latestSynced == nil || entry.workspace.LastSyncedAt.After(*latestSynced) {
				t := *entry.workspace.LastSyncedAt
				latestSynced = &t
			}
		}
	}

	// Use the last error from the most recently synced entry, if any.
	var lastError *string
	for _, entry := range group {
		if entry.workspace.LastError != nil {
			lastError = entry.workspace.LastError
		}
	}

	ws := &domain.Workspace{
		Name:          spec.Name,
		Repo:          repo,
		RepoRoot:      spec.RepoRoot,
		BareRoot:      spec.BareRoot,
		Owner:         spec.Owner,
		PatName:       spec.PatName,
		Template:      spec.Template,
		Worktrees:     worktrees,
		Events:        allEvents,
		IDE:           ideMap,
		ProvisionedAt: earliestProvisioned,
		LastError:     lastError,
		LastSyncedAt:  latestSynced,
	}

	// Re-project status from merged events.
	if len(allEvents) > 0 {
		var resolver domain.WorkspaceStatusResolver
		ws.Status = resolver.Resolve(allEvents)
	}

	return ws
}

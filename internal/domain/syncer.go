package domain

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type SyncReport struct {
	WorkspacesChecked int      `json:"workspaces_checked"`
	LifecycleChanges  []string `json:"lifecycle_changes"`
	Errors            []string `json:"errors"`
}

// HydrateReport is the structured response from a Hydrate() operation.
type HydrateReport struct {
	WorkspacesDiscovered   int      `json:"workspaces_discovered"`
	WorkspacesAlreadyKnown int      `json:"workspaces_already_known"`
	Errors                 []string `json:"errors"`
}

// BootReport is the structured response from a Boot() operation.
type BootReport struct {
	Hydrate *HydrateReport `json:"hydrate"`
	Sync    *SyncReport    `json:"sync"`
}

type WorkspaceSyncer struct {
	store         StateStore
	activityLog   *ActivityLog
	ideAdapter    IDEAdapter
	portAllocator *PortAllocator
}

func NewSyncer(store StateStore, activityLog *ActivityLog) *WorkspaceSyncer {
	return &WorkspaceSyncer{store: store, activityLog: activityLog}
}

// WithIDE configures the syncer to health-check IDE instances during sync.
func (s *WorkspaceSyncer) WithIDE(adapter IDEAdapter, pa *PortAllocator) *WorkspaceSyncer {
	s.ideAdapter = adapter
	s.portAllocator = pa
	return s
}

func (s *WorkspaceSyncer) Sync() (*SyncReport, error) {
	report := &SyncReport{}

	return report, s.store.WithLock(func() error {
		instances, err := s.store.Load()
		if err != nil {
			return err
		}

		now := time.Now().UTC()

		for name, ws := range instances {
			report.WorkspacesChecked++
			oldLifecycle := ws.Status

			// Check the default worktree's clone on disk
			defaultWT := ws.DefaultWorktree()
			if defaultWT == nil {
				// No worktrees registered — check if we can find one
				ws.LastSyncedAt = &now
				continue
			}

			cloneExists := false
			gitDir := filepath.Join(defaultWT.ProjectRoot, ".git")
			if _, statErr := os.Stat(gitDir); statErr == nil {
				cloneExists = true
				if ws.Status == StatusPending || ws.Status == StatusFailed {
					ws.RecordEvent(EventCloneDetected, "detected by sync")
					s.appendToActivityLog(ws.Events[len(ws.Events)-1])
					ws.LastError = nil
				}
			} else {
				if ws.Status == StatusReady {
					msg := "worktree missing from disk"
					ws.RecordEvent(EventProvisionFailed, msg)
					s.appendToActivityLog(ws.Events[len(ws.Events)-1])
					ws.LastError = &msg
				}
			}

			// Refresh head commit on default worktree
			if cloneExists {
				defaultWT.HeadCommit = ResolveHeadCommit(defaultWT.ProjectRoot, ws.Owner)
			} else {
				defaultWT.HeadCommit = ""
			}

			// IDE health-check (iterate all worktree IDE instances)
			for wtName, ide := range ws.IDE {
				if ide == nil || s.ideAdapter == nil {
					continue
				}
				wt := ws.FindWorktree(wtName)
				wtPath := ""
				if wt != nil {
					wtPath = wt.ProjectRoot
				}
				ctx := IDEContext{
					Owner:        ws.Owner,
					WorktreePath: wtPath,
					WorktreeName: wtName,
					Port:         ide.Port,
				}
				wasReady := ide.Status == StatusReady
				err := s.ideAdapter.HealthCheck(ctx)
				if err != nil && wasReady {
					ide.RecordEvent(IDEEventStopped, "health check failed")
					s.appendToActivityLog(ide.Events[len(ide.Events)-1])
				}
			}

			ws.LastSyncedAt = &now

			if ws.Status != oldLifecycle {
				change := fmt.Sprintf("%s: %s -> %s", name, oldLifecycle, ws.Status)
				report.LifecycleChanges = append(report.LifecycleChanges, change)
			}
		}

		return s.store.Save(instances)
	})
}

// Hydrate scans the workspace root directory for bare clones not yet tracked
// in state and reconstructs Workspace entries from disk. It merges discovered
// workspaces with existing state — entries already present with richer event
// history are not overwritten. Hydration is idempotent.
func (s *WorkspaceSyncer) Hydrate(workspaceRoot string) (*HydrateReport, error) {
	report := &HydrateReport{}

	return report, s.store.WithLock(func() error {
		instances, err := s.store.Load()
		if err != nil {
			return err
		}

		// Scan up to 4 levels deep for directories containing .bare/
		discovered, scanErrors := scanForBareClones(workspaceRoot, 4)
		report.Errors = append(report.Errors, scanErrors...)

		for _, entry := range discovered {
			wsName := filepath.Base(entry.repoRoot)

			// If already tracked, skip (preserve richer event history)
			if _, exists := instances[wsName]; exists {
				report.WorkspacesAlreadyKnown++
				continue
			}

			// Reconstruct RepoInfo from git remote
			repoInfo, err := resolveRepoInfoFromBare(entry.bareRoot, "")
			if err != nil {
				report.Errors = append(report.Errors,
					fmt.Sprintf("skipping %s: failed to resolve repo info: %v", wsName, err))
				continue
			}

			// Enumerate worktrees from the bare clone
			wtEntries, err := ListWorktreeEntries(entry.bareRoot, "")
			if err != nil {
				report.Errors = append(report.Errors,
					fmt.Sprintf("skipping %s: failed to list worktrees: %v", wsName, err))
				continue
			}

			var worktrees []Worktree
			for _, wte := range wtEntries {
				wtName := filepath.Base(wte.Path)
				isDefault := wtName == "default"
				headCommit := ResolveHeadCommit(wte.Path, "")
				worktrees = append(worktrees, Worktree{
					Name:        wtName,
					Branch:      wte.Branch,
					ProjectRoot: wte.Path,
					IsDefault:   isDefault,
					HeadCommit:  headCommit,
				})
			}

			ws := &Workspace{
				Name:      wsName,
				Repo:      repoInfo,
				RepoRoot:  entry.repoRoot,
				BareRoot:  entry.bareRoot,
				Worktrees: worktrees,
			}

			// Emit clone_detected event
			ws.RecordEvent(EventCloneDetected, "discovered by hydration")
			s.appendToActivityLog(ws.Events[len(ws.Events)-1])

			instances[wsName] = ws
			report.WorkspacesDiscovered++
		}

		return s.store.Save(instances)
	})
}

// Boot composes hydrate-then-sync: discover unknown workspaces from disk,
// then health-check all known workspaces (including newly hydrated ones).
// If hydration fails, boot aborts and returns the error.
func (s *WorkspaceSyncer) Boot(workspaceRoot string) (*BootReport, error) {
	hydrateReport, err := s.Hydrate(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("hydration failed: %w", err)
	}

	syncReport, err := s.Sync()
	if err != nil {
		return nil, fmt.Errorf("sync failed after hydration: %w", err)
	}

	return &BootReport{
		Hydrate: hydrateReport,
		Sync:    syncReport,
	}, nil
}

// bareCloneEntry represents a discovered bare clone on disk.
type bareCloneEntry struct {
	repoRoot string // parent directory of .bare/
	bareRoot string // path to .bare/ directory
}

// scanForBareClones walks the workspace root up to maxDepth levels deep
// looking for directories containing a .bare/ subdirectory.
// Returns discovered entries and any non-fatal errors encountered.
func scanForBareClones(root string, maxDepth int) ([]bareCloneEntry, []string) {
	var entries []bareCloneEntry
	var errors []string

	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth {
			return
		}

		dirEntries, err := os.ReadDir(dir)
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot read %s: %v", dir, err))
			return
		}

		for _, de := range dirEntries {
			if !de.IsDir() {
				continue
			}

			child := filepath.Join(dir, de.Name())

			if de.Name() == ".bare" {
				// Found a bare clone — the parent directory is the repo root
				entries = append(entries, bareCloneEntry{
					repoRoot: dir,
					bareRoot: child,
				})
				return // don't recurse into .bare/ or its siblings
			}
		}

		// No .bare/ found at this level — recurse into subdirectories
		for _, de := range dirEntries {
			if !de.IsDir() {
				continue
			}
			name := de.Name()
			// Skip hidden dirs (except .bare which we already handle above)
			if strings.HasPrefix(name, ".") {
				continue
			}
			walk(filepath.Join(dir, name), depth+1)
		}
	}

	walk(root, 0)
	return entries, errors
}

// resolveRepoInfoFromBare extracts RepoInfo from a bare clone by reading
// git remote origin URL. The owner parameter is used for su when running
// as root. Returns an error if the remote URL cannot be read or parsed.
func resolveRepoInfoFromBare(bareRoot, owner string) (RepoInfo, error) {
	gitCmd := fmt.Sprintf("git -C %s remote get-url origin", bareRoot)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", gitCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "remote", "get-url", "origin")
	}

	out, err := cmd.Output()
	if err != nil {
		return RepoInfo{}, fmt.Errorf("git remote get-url origin failed: %w", err)
	}

	cloneURL := strings.TrimSpace(string(out))
	return ParseOriginURL(cloneURL)
}

// ParseOriginURL parses a git clone URL into a RepoInfo.
// Supports HTTPS (https://github.com/org/repo.git) and
// SSH (git@github.com:org/repo.git) URL formats.
func ParseOriginURL(rawURL string) (RepoInfo, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return RepoInfo{}, fmt.Errorf("empty clone URL")
	}

	var host, slug string

	// SSH format: git@github.com:org/repo.git
	if strings.Contains(rawURL, "://") {
		// HTTPS format
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return RepoInfo{}, fmt.Errorf("invalid URL %q: %w", rawURL, err)
		}
		host = parsed.Host
		slug = strings.TrimPrefix(parsed.Path, "/")
	} else if idx := strings.Index(rawURL, ":"); idx > 0 && strings.Contains(rawURL, "@") {
		// SSH format: user@host:path
		atIdx := strings.Index(rawURL, "@")
		host = rawURL[atIdx+1 : idx]
		slug = rawURL[idx+1:]
	} else {
		return RepoInfo{}, fmt.Errorf("unrecognized URL format: %q", rawURL)
	}

	// Strip .git suffix
	slug = strings.TrimSuffix(slug, ".git")
	// Strip trailing slash
	slug = strings.TrimSuffix(slug, "/")

	if host == "" || slug == "" {
		return RepoInfo{}, fmt.Errorf("could not extract host/slug from %q", rawURL)
	}

	return RepoInfo{
		Host:     host,
		Slug:     slug,
		CloneURL: rawURL,
	}, nil
}

// appendToActivityLog writes an event record to the activity log if configured.
func (s *WorkspaceSyncer) appendToActivityLog(record EventRecord) {
	if s.activityLog == nil {
		return
	}
	_ = s.activityLog.Append(record)
}

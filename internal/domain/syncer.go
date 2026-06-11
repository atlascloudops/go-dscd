package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type SyncReport struct {
	WorkspacesChecked int      `json:"workspaces_checked"`
	LifecycleChanges  []string `json:"lifecycle_changes"`
	Errors            []string `json:"errors"`
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

// appendToActivityLog writes an event record to the activity log if configured.
func (s *WorkspaceSyncer) appendToActivityLog(record EventRecord) {
	if s.activityLog == nil {
		return
	}
	_ = s.activityLog.Append(record)
}

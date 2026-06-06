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

		for name, inst := range instances {
			report.WorkspacesChecked++
			oldLifecycle := inst.Status

			// Check clone — .git may be a directory (traditional clone) or
			// a file (worktree with gitdir: pointer). Either means the
			// workspace exists on disk.
			cloneExists := false
			gitDir := filepath.Join(inst.Spec.ProjectRoot, ".git")
			if _, statErr := os.Stat(gitDir); statErr == nil {
				cloneExists = true
				if inst.Status == StatusPending || inst.Status == StatusFailed {
					// Workspace appeared on disk — emit synthetic worktree_created
					inst.RecordEvent(EventCloneDetected, "detected by sync")
					s.appendToActivityLog(inst.Events[len(inst.Events)-1])
					inst.LastError = nil
				}
			} else {
				if inst.Status == StatusReady {
					msg := "worktree missing from disk"
					inst.RecordEvent(EventProvisionFailed, msg)
					s.appendToActivityLog(inst.Events[len(inst.Events)-1])
					inst.LastError = &msg
				}
			}

			// Refresh head commit
			if cloneExists {
				inst.HeadCommit = ResolveHeadCommit(inst.Spec.ProjectRoot, inst.Spec.Owner)
			} else {
				inst.HeadCommit = ""
			}

			// IDE health-check
			if inst.IDE != nil && s.ideAdapter != nil {
				ctx := IDEContext{
					Owner:        inst.Spec.Owner,
					WorktreePath: inst.Spec.ProjectRoot,
					WorktreeName: inst.Spec.WorktreeName,
					Port:         inst.IDE.Port,
				}
				wasReady := inst.IDE.Status == StatusReady
				err := s.ideAdapter.HealthCheck(ctx)
				if err != nil && wasReady {
					inst.IDE.RecordEvent(IDEEventStopped, "health check failed")
					s.appendToActivityLog(inst.IDE.Events[len(inst.IDE.Events)-1])
				}
			}

			inst.LastSyncedAt = &now

			if inst.Status != oldLifecycle {
				change := fmt.Sprintf("%s: %s -> %s", name, oldLifecycle, inst.Status)
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
	// Best-effort: activity log write failures are non-fatal for sync.
	_ = s.activityLog.Append(record)
}

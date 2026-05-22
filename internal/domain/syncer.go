package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SyncReport struct {
	WorkspacesChecked int      `json:"workspaces_checked"`
	LifecycleChanges  []string `json:"lifecycle_changes"`
	Errors            []string `json:"errors"`
}

type WorkspaceSyncer struct {
	store  StateStore
	logDir string
}

func NewSyncer(store StateStore, logDir string) *WorkspaceSyncer {
	return &WorkspaceSyncer{store: store, logDir: logDir}
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
			oldLifecycle := inst.Lifecycle

			// Check clone — .git may be a directory (traditional clone) or
			// a file (worktree with gitdir: pointer). Either means the
			// workspace exists on disk.
			cloneExists := false
			gitDir := filepath.Join(inst.Spec.ProjectRoot, ".git")
			if _, statErr := os.Stat(gitDir); statErr == nil {
				cloneExists = true
				if inst.Lifecycle == LifecyclePending || inst.Lifecycle == LifecycleFailed {
					// Workspace appeared on disk — emit synthetic worktree_created
					appendEvent(inst, EventCloneDetected, "detected by sync")
					inst.LastError = nil
				}
			} else {
				if inst.Lifecycle == LifecycleReady {
					msg := "worktree missing from disk"
					appendEvent(inst, EventProvisionFailed, msg)
					inst.LastError = &msg
				}
			}

			// Check credentials
			credPath := filepath.Join("/home", inst.Spec.Owner, ".config/dsc/credentials/git-credentials")
			data, credErr := os.ReadFile(credPath)
			if credErr == nil && strings.Contains(string(data), inst.Spec.VCS.Host) {
				inst.CredentialFresh = true
			} else {
				inst.CredentialFresh = false
			}

			// Refresh head commit
			if cloneExists {
				inst.HeadCommit = ResolveHeadCommit(inst.Spec.ProjectRoot, inst.Spec.Owner)
			} else {
				inst.HeadCommit = ""
			}

			inst.LastSyncedAt = &now

			if inst.Lifecycle != oldLifecycle {
				change := fmt.Sprintf("%s: %s -> %s", name, oldLifecycle, inst.Lifecycle)
				report.LifecycleChanges = append(report.LifecycleChanges, change)
				s.writeLog(name, "sync", "%s", change)
			} else {
				s.writeLog(name, "sync", "Clone exists=%t, lifecycle confirmed: %s", cloneExists, inst.Lifecycle)
			}
		}

		return s.store.Save(instances)
	})
}

func (s *WorkspaceSyncer) writeLog(name, phase, format string, args ...interface{}) {
	if s.logDir == "" {
		return
	}
	os.MkdirAll(s.logDir, 0755)
	logPath := filepath.Join(s.logDir, name+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(f, "[%s] [%s] %s\n", ts, phase, msg)
}

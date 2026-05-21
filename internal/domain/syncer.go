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
	StateChanges      []string `json:"state_changes"`
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
			oldState := inst.State

			// Check clone — .git may be a directory (traditional clone) or
		// a file (worktree with gitdir: pointer). Either means the
		// workspace exists on disk.
			gitDir := filepath.Join(inst.Spec.ProjectRoot, ".git")
			if _, statErr := os.Stat(gitDir); statErr == nil {
				inst.CloneExists = true
				if inst.State == StatePending || inst.State == StateError {
					inst.State = StateReady
					inst.LastError = nil
				}
			} else {
				inst.CloneExists = false
				if inst.State == StateReady {
					inst.State = StateError
					msg := "worktree missing from disk"
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

			// Refresh head commit and derive status
			if inst.CloneExists {
				inst.HeadCommit = ResolveHeadCommit(inst.Spec.ProjectRoot, inst.Spec.Owner)
			} else {
				inst.HeadCommit = ""
			}
			inst.DeriveStatus()

			inst.LastSyncedAt = &now

			if inst.State != oldState {
				change := fmt.Sprintf("%s: %s -> %s", name, oldState, inst.State)
				report.StateChanges = append(report.StateChanges, change)
				s.writeLog(name, "sync", "%s", change)
			} else {
				s.writeLog(name, "sync", "Clone exists=%t, state confirmed: %s", inst.CloneExists, inst.State)
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

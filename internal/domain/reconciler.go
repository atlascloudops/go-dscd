package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReconcileReport struct {
	WorkspacesChecked int      `json:"workspaces_checked"`
	StateChanges      []string `json:"state_changes"`
	Errors            []string `json:"errors"`
}

type WorkspaceReconciler struct {
	store  StateStore
	logDir string
}

func NewReconciler(store StateStore, logDir string) *WorkspaceReconciler {
	return &WorkspaceReconciler{store: store, logDir: logDir}
}

func (r *WorkspaceReconciler) Reconcile() (*ReconcileReport, error) {
	report := &ReconcileReport{}

	return report, r.store.WithLock(func() error {
		instances, err := r.store.Load()
		if err != nil {
			return err
		}

		now := time.Now().UTC()

		for name, inst := range instances {
			report.WorkspacesChecked++
			oldState := inst.State

			// Check clone
			gitDir := filepath.Join(inst.Spec.ProjectRoot, ".git")
			if info, statErr := os.Stat(gitDir); statErr == nil && info.IsDir() {
				inst.CloneExists = true
				if inst.State == StatePending || inst.State == StateError {
					inst.State = StateReady
					inst.LastError = nil
				}
			} else {
				inst.CloneExists = false
				if inst.State == StateReady {
					inst.State = StateError
					msg := "clone directory missing after reboot"
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

			inst.LastReconcileAt = &now

			if inst.State != oldState {
				change := fmt.Sprintf("%s: %s -> %s", name, oldState, inst.State)
				report.StateChanges = append(report.StateChanges, change)
				r.writeLog(name, "reconcile", "%s", change)
			} else {
				r.writeLog(name, "reconcile", "Clone exists=%t, state confirmed: %s", inst.CloneExists, inst.State)
			}
		}

		return r.store.Save(instances)
	})
}

func (r *WorkspaceReconciler) writeLog(name, phase, format string, args ...interface{}) {
	if r.logDir == "" {
		return
	}
	os.MkdirAll(r.logDir, 0755)
	logPath := filepath.Join(r.logDir, name+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(f, "[%s] [%s] %s\n", ts, phase, msg)
}

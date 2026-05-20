package domain

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReconcile_PendingWithClone(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StatePending,
	}

	r := NewReconciler(store, filepath.Join(dir, "logs"))
	report, err := r.Reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if report.WorkspacesChecked != 1 {
		t.Fatalf("expected 1 checked, got %d", report.WorkspacesChecked)
	}
	if len(report.StateChanges) != 1 {
		t.Fatalf("expected 1 state change, got %d", len(report.StateChanges))
	}
	if store.instances["ws1"].State != StateReady {
		t.Fatalf("expected ready, got %s", store.instances["ws1"].State)
	}
}

func TestReconcile_ReadyWithoutClone(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "missing-repo")

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StateReady,
	}

	r := NewReconciler(store, filepath.Join(dir, "logs"))
	report, err := r.Reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if store.instances["ws1"].State != StateError {
		t.Fatalf("expected error, got %s", store.instances["ws1"].State)
	}
	if store.instances["ws1"].LastError == nil {
		t.Fatal("expected last_error to be set")
	}
	if len(report.StateChanges) != 1 {
		t.Fatalf("expected 1 state change, got %d", len(report.StateChanges))
	}
}

func TestReconcile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StateReady,
	}

	r := NewReconciler(store, filepath.Join(dir, "logs"))

	report1, _ := r.Reconcile()
	report2, _ := r.Reconcile()

	if len(report1.StateChanges) != 0 {
		t.Fatalf("first reconcile should have no changes for ready+exists, got %v", report1.StateChanges)
	}
	if len(report2.StateChanges) != 0 {
		t.Fatalf("second reconcile should have no changes, got %v", report2.StateChanges)
	}
}

func TestReconcile_SetsTimestamp(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StateReady,
	}

	before := time.Now().UTC()
	r := NewReconciler(store, filepath.Join(dir, "logs"))
	r.Reconcile()

	ts := store.instances["ws1"].LastReconcileAt
	if ts == nil {
		t.Fatal("expected last_reconcile_at to be set")
	}
	if ts.Before(before) {
		t.Fatal("timestamp should be >= before")
	}
}

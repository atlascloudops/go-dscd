package domain

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSync_PendingWithClone(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StatePending,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
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

func TestSync_ReadyWithoutClone(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "missing-repo")

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StateReady,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
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

func TestSync_Idempotent(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StateReady,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))

	report1, _ := s.Sync()
	report2, _ := s.Sync()

	if len(report1.StateChanges) != 0 {
		t.Fatalf("first sync should have no changes for ready+exists, got %v", report1.StateChanges)
	}
	if len(report2.StateChanges) != 0 {
		t.Fatalf("second sync should have no changes, got %v", report2.StateChanges)
	}
}

func TestSync_SetsTimestamp(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StateReady,
	}

	before := time.Now().UTC()
	s := NewSyncer(store, filepath.Join(dir, "logs"))
	s.Sync()

	ts := store.instances["ws1"].LastSyncedAt
	if ts == nil {
		t.Fatal("expected last_synced_at to be set")
	}
	if ts.Before(before) {
		t.Fatal("timestamp should be >= before")
	}
}

func TestSync_PendingWithWorktreeGitFile(t *testing.T) {
	// Worktree-native .git is a file (gitdir: pointer), not a directory.
	// Sync must detect this as an existing workspace.
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo", ".worktrees", "feature")
	os.MkdirAll(projectRoot, 0755)
	os.WriteFile(filepath.Join(projectRoot, ".git"), []byte("gitdir: ../../.bare/worktrees/feature\n"), 0644)

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StatePending,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
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
	if !store.instances["ws1"].CloneExists {
		t.Fatal("expected clone_exists=true for worktree .git file")
	}
}

func TestSync_ReadyWorktreeRemovedFromDisk(t *testing.T) {
	// If a worktree's .git file disappears, sync should mark it as error.
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo", ".worktrees", "gone")
	// Don't create anything — simulates deleted worktree

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:  WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		State: StateReady,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if store.instances["ws1"].State != StateError {
		t.Fatalf("expected error, got %s", store.instances["ws1"].State)
	}
	if store.instances["ws1"].LastError == nil {
		t.Fatal("expected last_error to be set")
	}
	if *store.instances["ws1"].LastError != "worktree missing from disk" {
		t.Fatalf("expected 'worktree missing from disk', got %q", *store.instances["ws1"].LastError)
	}
	if len(report.StateChanges) != 1 {
		t.Fatalf("expected 1 state change, got %d", len(report.StateChanges))
	}
}

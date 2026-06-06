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
	store.instances["ws1"] = &Workspace{
		Spec:      WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusPending,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if report.WorkspacesChecked != 1 {
		t.Fatalf("expected 1 checked, got %d", report.WorkspacesChecked)
	}
	if len(report.LifecycleChanges) != 1 {
		t.Fatalf("expected 1 lifecycle change, got %d", len(report.LifecycleChanges))
	}
	if store.instances["ws1"].Status != StatusReady {
		t.Fatalf("expected ready, got %s", store.instances["ws1"].Status)
	}
	// Should have emitted a clone_detected event
	lastEvent := store.instances["ws1"].Events[len(store.instances["ws1"].Events)-1]
	if lastEvent.Event != string(EventCloneDetected) {
		t.Fatalf("expected clone_detected event, got %s", lastEvent.Event)
	}
}

func TestSync_ReadyWithoutClone(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "missing-repo")

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:      WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if store.instances["ws1"].Status != StatusFailed {
		t.Fatalf("expected failed, got %s", store.instances["ws1"].Status)
	}
	if store.instances["ws1"].LastError == nil {
		t.Fatal("expected last_error to be set")
	}
	if len(report.LifecycleChanges) != 1 {
		t.Fatalf("expected 1 lifecycle change, got %d", len(report.LifecycleChanges))
	}
}

func TestSync_Idempotent(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:      WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))

	report1, _ := s.Sync()
	report2, _ := s.Sync()

	if len(report1.LifecycleChanges) != 0 {
		t.Fatalf("first sync should have no changes for ready+exists, got %v", report1.LifecycleChanges)
	}
	if len(report2.LifecycleChanges) != 0 {
		t.Fatalf("second sync should have no changes, got %v", report2.LifecycleChanges)
	}
}

func TestSync_SetsTimestamp(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:      WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
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
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo", ".worktrees", "feature")
	os.MkdirAll(projectRoot, 0755)
	os.WriteFile(filepath.Join(projectRoot, ".git"), []byte("gitdir: ../../.bare/worktrees/feature\n"), 0644)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:      WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusPending,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if report.WorkspacesChecked != 1 {
		t.Fatalf("expected 1 checked, got %d", report.WorkspacesChecked)
	}
	if len(report.LifecycleChanges) != 1 {
		t.Fatalf("expected 1 lifecycle change, got %d", len(report.LifecycleChanges))
	}
	if store.instances["ws1"].Status != StatusReady {
		t.Fatalf("expected ready, got %s", store.instances["ws1"].Status)
	}
}

func TestSync_ReadyWorktreeRemovedFromDisk(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo", ".worktrees", "gone")

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:      WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}
	if store.instances["ws1"].Status != StatusFailed {
		t.Fatalf("expected failed, got %s", store.instances["ws1"].Status)
	}
	if store.instances["ws1"].LastError == nil {
		t.Fatal("expected last_error to be set")
	}
	if *store.instances["ws1"].LastError != "worktree missing from disk" {
		t.Fatalf("expected 'worktree missing from disk', got %q", *store.instances["ws1"].LastError)
	}
	if len(report.LifecycleChanges) != 1 {
		t.Fatalf("expected 1 lifecycle change, got %d", len(report.LifecycleChanges))
	}
}

func TestSync_CloneDetectedEvent(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:      WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusFailed,
	}

	s := NewSyncer(store, filepath.Join(dir, "logs"))
	s.Sync()

	if store.instances["ws1"].Status != StatusReady {
		t.Fatalf("expected ready after clone detected, got %s", store.instances["ws1"].Status)
	}
	lastEvent := store.instances["ws1"].Events[len(store.instances["ws1"].Events)-1]
	if lastEvent.Event != string(EventCloneDetected) {
		t.Fatalf("expected clone_detected event, got %s", lastEvent.Event)
	}
	if lastEvent.Detail != "detected by sync" {
		t.Fatalf("expected detail 'detected by sync', got %q", lastEvent.Detail)
	}
}

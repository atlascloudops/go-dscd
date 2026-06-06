package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubIDEAdapter is a minimal IDEAdapter for testing syncer IDE health-check paths.
type stubIDEAdapter struct {
	healthErr error
}

func (s *stubIDEAdapter) Name() string                    { return "stub" }
func (s *stubIDEAdapter) Start(_ IDEContext) error        { return nil }
func (s *stubIDEAdapter) Stop(_ IDEContext) error         { return nil }
func (s *stubIDEAdapter) HealthCheck(_ IDEContext) error  { return s.healthErr }

func TestSync_PendingWithClone(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusPending,
	}

	actLog := NewActivityLog(filepath.Join(dir, "activity.log"))
	s := NewSyncer(store, actLog)
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
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	actLog := NewActivityLog(filepath.Join(dir, "activity.log"))
	s := NewSyncer(store, actLog)
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
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	s := NewSyncer(store, nil)

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
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	before := time.Now().UTC()
	s := NewSyncer(store, nil)
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
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusPending,
	}

	s := NewSyncer(store, nil)
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
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	s := NewSyncer(store, nil)
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
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusFailed,
	}

	s := NewSyncer(store, nil)
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

func TestSync_EventRecordHasCorrectScope(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusPending,
	}

	s := NewSyncer(store, nil)
	s.Sync()

	events := store.instances["ws1"].Events
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	lastEvent := events[len(events)-1]
	if lastEvent.Scope.Kind != ScopeKindWorkspace {
		t.Fatalf("expected scope kind %q, got %q", ScopeKindWorkspace, lastEvent.Scope.Kind)
	}
	if lastEvent.Scope.Name != "ws1" {
		t.Fatalf("expected scope name %q, got %q", "ws1", lastEvent.Scope.Name)
	}
}

func TestSync_ProvisionFailedEventHasCorrectScope(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "missing")

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
	}

	s := NewSyncer(store, nil)
	s.Sync()

	events := store.instances["ws1"].Events
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	lastEvent := events[len(events)-1]
	if lastEvent.Event != string(EventProvisionFailed) {
		t.Fatalf("expected provision_failed event, got %s", lastEvent.Event)
	}
	if lastEvent.Scope.Kind != ScopeKindWorkspace {
		t.Fatalf("expected scope kind %q, got %q", ScopeKindWorkspace, lastEvent.Scope.Kind)
	}
}

func TestSync_ActivityLogReceivesWorkspaceEvents(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusPending,
	}

	actLog := NewActivityLog(filepath.Join(dir, "activity.log"))
	s := NewSyncer(store, actLog)
	s.Sync()

	records, err := actLog.Read(ActivityLogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 activity log record, got %d", len(records))
	}
	if records[0].Event != string(EventCloneDetected) {
		t.Fatalf("expected clone_detected in activity log, got %s", records[0].Event)
	}
	if records[0].Scope.Kind != ScopeKindWorkspace {
		t.Fatalf("expected workspace scope in activity log, got %s", records[0].Scope.Kind)
	}
}

func TestSync_ActivityLogReceivesIDEEvents(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", WorktreeName: "default", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
		IDE: &IDEInstance{
			Name:    "ws1",
			Adapter: "openvscode-server",
			Port:    18080,
			Status:  StatusReady,
		},
	}

	actLog := NewActivityLog(filepath.Join(dir, "activity.log"))
	failingAdapter := &stubIDEAdapter{healthErr: fmt.Errorf("connection refused")}
	s := NewSyncer(store, actLog)
	s.WithIDE(failingAdapter, nil)
	s.Sync()

	records, err := actLog.Read(ActivityLogFilter{ScopeKind: ScopeKindIDE})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 IDE activity log record, got %d", len(records))
	}
	if records[0].Event != string(IDEEventStopped) {
		t.Fatalf("expected ide_stopped in activity log, got %s", records[0].Event)
	}
}

func TestSync_NilActivityLogDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	store.instances["ws1"] = &Workspace{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusPending,
	}

	s := NewSyncer(store, nil)
	_, err := s.Sync()
	if err != nil {
		t.Fatalf("sync with nil activity log should not error: %v", err)
	}
}

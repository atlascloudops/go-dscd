package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Test doubles ---

type mockSystemdRunner struct {
	started  []string
	stopped  []string
	active   map[string]bool
	startErr error
	stopErr  error
}

func newMockSystemdRunner() *mockSystemdRunner {
	return &mockSystemdRunner{active: make(map[string]bool)}
}

func (m *mockSystemdRunner) Start(unit string) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = append(m.started, unit)
	m.active[unit] = true
	return nil
}

func (m *mockSystemdRunner) Stop(unit string) error {
	if m.stopErr != nil {
		return m.stopErr
	}
	m.stopped = append(m.stopped, unit)
	delete(m.active, unit)
	return nil
}

func (m *mockSystemdRunner) IsActive(unit string) (bool, error) {
	return m.active[unit], nil
}

type mockHTTPChecker struct {
	healthy bool
	err     error
}

func (m *mockHTTPChecker) Check(url string) error {
	if m.healthy {
		return nil
	}
	if m.err != nil {
		return m.err
	}
	return errors.New("not ready")
}

// --- Tests ---

func TestCodeServerAdapter_Name(t *testing.T) {
	a := NewCodeServerAdapter()
	if a.Name() != "openvscode-server" {
		t.Errorf("expected openvscode-server, got %s", a.Name())
	}
}

func TestUnitName(t *testing.T) {
	ctx := IDEContext{Owner: "jperez", WorktreeName: "default"}
	got := UnitName(ctx)
	want := "openvscode-server@jperez--default.service"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestUnitName_BranchWorktree(t *testing.T) {
	ctx := IDEContext{Owner: "alice", WorktreeName: "feature-vpc"}
	got := UnitName(ctx)
	want := "openvscode-server@alice--feature-vpc.service"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCodeServerAdapter_Start(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()
	checker := &mockHTTPChecker{healthy: true}

	a := &CodeServerAdapter{
		EnvDir:        dir,
		SystemdRunner: runner,
		HTTPChecker:   checker,
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}

	ctx := IDEContext{
		Owner:        "jperez",
		WorktreePath: "/home/jperez/code/myrepo/default",
		WorktreeName: "default",
		Port:         9100,
	}

	if err := a.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify systemd unit was started
	expectedUnit := "openvscode-server@jperez--default.service"
	if len(runner.started) != 1 || runner.started[0] != expectedUnit {
		t.Errorf("expected unit %q started, got %v", expectedUnit, runner.started)
	}

	// Verify env file was written
	envPath := filepath.Join(dir, "jperez--default.env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "IDE_OWNER=jperez") {
		t.Error("env file missing IDE_OWNER")
	}
	if !strings.Contains(content, "IDE_PORT=9100") {
		t.Error("env file missing IDE_PORT")
	}
	if !strings.Contains(content, "IDE_WORKTREE_PATH=/home/jperez/code/myrepo/default") {
		t.Error("env file missing IDE_WORKTREE_PATH")
	}
}

func TestCodeServerAdapter_StartTimeout(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()
	checker := &mockHTTPChecker{healthy: false, err: errors.New("connection refused")}

	a := &CodeServerAdapter{
		EnvDir:        dir,
		SystemdRunner: runner,
		HTTPChecker:   checker,
		PollTimeout:   100 * time.Millisecond,
		PollInterval:  10 * time.Millisecond,
	}

	ctx := IDEContext{Owner: "alice", WorktreeName: "default", WorktreePath: "/tmp/wt", Port: 9100}
	err := a.Start(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "did not become ready") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCodeServerAdapter_Stop(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()

	a := &CodeServerAdapter{
		EnvDir:        dir,
		SystemdRunner: runner,
		HTTPChecker:   &mockHTTPChecker{healthy: true},
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}

	ctx := IDEContext{Owner: "jperez", WorktreeName: "default", WorktreePath: "/tmp/wt", Port: 9100}

	// Create the env file first
	envPath := filepath.Join(dir, "jperez--default.env")
	os.WriteFile(envPath, []byte("IDE_PORT=9100\n"), 0644)

	if err := a.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify unit was stopped
	expectedUnit := "openvscode-server@jperez--default.service"
	if len(runner.stopped) != 1 || runner.stopped[0] != expectedUnit {
		t.Errorf("expected unit %q stopped, got %v", expectedUnit, runner.stopped)
	}

	// Verify env file was removed
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Error("expected env file to be removed")
	}
}

func TestCodeServerAdapter_StopMissingEnvFile(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()

	a := &CodeServerAdapter{
		EnvDir:        dir,
		SystemdRunner: runner,
		HTTPChecker:   &mockHTTPChecker{},
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}

	ctx := IDEContext{Owner: "alice", WorktreeName: "default", WorktreePath: "/tmp/wt", Port: 9100}

	// Should not error even if env file doesn't exist
	if err := a.Stop(ctx); err != nil {
		t.Errorf("Stop with missing env file should succeed, got: %v", err)
	}
}

func TestCodeServerAdapter_HealthCheck(t *testing.T) {
	a := &CodeServerAdapter{
		HTTPChecker: &mockHTTPChecker{healthy: true},
	}
	ctx := IDEContext{Port: 9100}
	if err := a.HealthCheck(ctx); err != nil {
		t.Errorf("expected healthy, got: %v", err)
	}
}

func TestCodeServerAdapter_HealthCheckUnhealthy(t *testing.T) {
	a := &CodeServerAdapter{
		HTTPChecker: &mockHTTPChecker{healthy: false, err: errors.New("connection refused")},
	}
	ctx := IDEContext{Port: 9100}
	if err := a.HealthCheck(ctx); err == nil {
		t.Error("expected error for unhealthy check")
	}
}

func TestCodeServerAdapter_ImplementsInterface(t *testing.T) {
	// Compile-time check that CodeServerAdapter satisfies IDEAdapter
	var _ IDEAdapter = (*CodeServerAdapter)(nil)
}

func TestIDEInstance_JSONRoundTrip(t *testing.T) {
	instance := IDEInstance{
		Adapter: "openvscode-server",
		Port:    9100,
		Events: []IDEEventRecord{
			{Event: IDEEventStarted, Timestamp: time.Now().UTC().Truncate(time.Second), Detail: "port=9100"},
			{Event: IDEEventReady, Timestamp: time.Now().UTC().Truncate(time.Second), Detail: "port=9100"},
		},
		Status: StatusReady,
	}

	data, err := json.Marshal(instance)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got IDEInstance
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Adapter != instance.Adapter {
		t.Errorf("adapter: expected %q, got %q", instance.Adapter, got.Adapter)
	}
	if got.Port != instance.Port {
		t.Errorf("port: expected %d, got %d", instance.Port, got.Port)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events: expected 2, got %d", len(got.Events))
	}
	if got.Status != StatusReady {
		t.Errorf("status: expected %q, got %q", StatusReady, got.Status)
	}
}

func TestWorkspaceInstance_IDEInstanceJSON(t *testing.T) {
	inst := WorkspaceInstance{
		Status: StatusPending,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Events: []IDEEventRecord{
				{Event: IDEEventStarted, Timestamp: time.Now().UTC().Truncate(time.Second)},
				{Event: IDEEventReady, Timestamp: time.Now().UTC().Truncate(time.Second)},
			},
			Status: StatusReady,
		},
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got WorkspaceInstance
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.IDE == nil {
		t.Fatal("IDE should not be nil after round-trip")
	}
	if got.IDE.Port != 9100 {
		t.Errorf("IDE.Port: expected 9100, got %d", got.IDE.Port)
	}
	if got.IDE.Adapter != "openvscode-server" {
		t.Errorf("IDE.Adapter: expected openvscode-server, got %q", got.IDE.Adapter)
	}
	if got.IDE.Status != StatusReady {
		t.Errorf("IDE.Status: expected %q, got %q", StatusReady, got.IDE.Status)
	}
	if len(got.IDE.Events) != 2 {
		t.Errorf("IDE.Events: expected 2, got %d", len(got.IDE.Events))
	}
}

func TestWorkspaceInstance_IDEInstanceOmittedWhenNil(t *testing.T) {
	inst := WorkspaceInstance{
		Status: StatusPending,
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	if _, ok := raw["ide"]; ok {
		t.Error("expected ide to be omitted from JSON when nil")
	}
}

// --- IDE Provisioning Phase Tests ---

func TestProvision_WithIDE_StartsAdapter(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()
	checker := &mockHTTPChecker{healthy: true}
	portFile := filepath.Join(dir, "ports.json")

	adapter := &CodeServerAdapter{
		EnvDir:        filepath.Join(dir, "env"),
		SystemdRunner: runner,
		HTTPChecker:   checker,
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}
	pa := NewPortAllocator(portFile)

	store := newMemStore()

	// Create a fake worktree on disk
	projectRoot := filepath.Join(dir, "repo", "default")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "fake", Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     filepath.Join(dir, "repo"),
		BareRoot:     filepath.Join(dir, "repo", ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		IDE:          &IDESpecConfig{Adapter: "openvscode-server"},
	}

	p := &Provisioner{
		LogDir:        filepath.Join(dir, "logs"),
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// IDE should be started and ready
	if inst.IDE == nil {
		t.Fatal("expected IDE instance to be set")
	}
	if inst.IDE.Status != StatusReady {
		t.Errorf("expected IDE status ready, got %s", inst.IDE.Status)
	}
	if inst.IDE.Port < 9100 || inst.IDE.Port > 9199 {
		t.Errorf("expected port in range 9100-9199, got %d", inst.IDE.Port)
	}
	if inst.IDE.Adapter != "openvscode-server" {
		t.Errorf("expected adapter 'openvscode-server', got %q", inst.IDE.Adapter)
	}

	// Should have emitted ide_started and ide_ready events in the IDE event stream
	hasStarted, hasReady := false, false
	for _, ev := range inst.IDE.Events {
		if ev.Event == IDEEventStarted {
			hasStarted = true
		}
		if ev.Event == IDEEventReady {
			hasReady = true
		}
	}
	if !hasStarted {
		t.Error("expected ide_started event")
	}
	if !hasReady {
		t.Error("expected ide_ready event")
	}

	// Workspace status should still be Ready (IDE events are in separate stream)
	if inst.Status != StatusReady {
		t.Errorf("expected status ready, got %s", inst.Status)
	}
}

func TestProvision_WithIDE_FailureNonFatal(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()
	runner.startErr = errors.New("systemd not available")
	checker := &mockHTTPChecker{healthy: false}
	portFile := filepath.Join(dir, "ports.json")

	adapter := &CodeServerAdapter{
		EnvDir:        filepath.Join(dir, "env"),
		SystemdRunner: runner,
		HTTPChecker:   checker,
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}
	pa := NewPortAllocator(portFile)

	store := newMemStore()
	projectRoot := filepath.Join(dir, "repo", "default")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "fake", Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     filepath.Join(dir, "repo"),
		BareRoot:     filepath.Join(dir, "repo", ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		IDE:          &IDESpecConfig{Adapter: "openvscode-server"},
	}

	p := &Provisioner{
		LogDir:        filepath.Join(dir, "logs"),
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision should succeed even when IDE fails: %v", err)
	}

	// IDE instance should exist with failed status
	if inst.IDE == nil {
		t.Fatal("expected IDE instance to be set even on failure")
	}
	if inst.IDE.Status != StatusFailed {
		t.Errorf("expected IDE status failed, got %s", inst.IDE.Status)
	}

	// Should have ide_failed event in the IDE event stream
	hasFailed := false
	for _, ev := range inst.IDE.Events {
		if ev.Event == IDEEventFailed {
			hasFailed = true
		}
	}
	if !hasFailed {
		t.Error("expected ide_failed event")
	}

	// Workspace status should still be Ready
	if inst.Status != StatusReady {
		t.Errorf("expected status ready despite IDE failure, got %s", inst.Status)
	}
}

func TestProvision_WithoutIDE_SkipsIDEPhase(t *testing.T) {
	dir := t.TempDir()
	store := newMemStore()
	projectRoot := filepath.Join(dir, "repo", "default")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "fake", Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     filepath.Join(dir, "repo"),
		BareRoot:     filepath.Join(dir, "repo", ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		// IDE is nil — no IDE requested
	}

	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	if inst.IDE != nil {
		t.Error("expected no IDE instance when IDE not requested")
	}
}

func TestProvision_InvalidAdapterName(t *testing.T) {
	dir := t.TempDir()
	store := newMemStore()

	adapter := &CodeServerAdapter{
		EnvDir:        filepath.Join(dir, "env"),
		SystemdRunner: newMockSystemdRunner(),
		HTTPChecker:   &mockHTTPChecker{},
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}

	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "fake", Branch: "main"},
		ProjectRoot:  filepath.Join(dir, "repo", "default"),
		RepoRoot:     filepath.Join(dir, "repo"),
		BareRoot:     filepath.Join(dir, "repo", ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		IDE:          &IDESpecConfig{Adapter: "unknown-adapter"},
	}

	p := &Provisioner{
		LogDir:        filepath.Join(dir, "logs"),
		IDEAdapter:    adapter,
		PortAllocator: NewPortAllocator(filepath.Join(dir, "ports.json")),
	}

	_, err := p.Provision(store, spec)
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrSpecInvalid {
		t.Errorf("expected SPEC_INVALID, got %s", pe.Code)
	}
}

func TestSync_IDEHealthCheck(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	checker := &mockHTTPChecker{healthy: true}
	adapter := &CodeServerAdapter{
		HTTPChecker: checker,
	}

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", WorktreeName: "default", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Status:  StatusReady,
		},
	}

	s := NewSyncer(store, filepath.Join(dir, "logs")).WithIDE(adapter, nil)
	_, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}

	// IDE should still be ready
	if store.instances["ws1"].IDE.Status != StatusReady {
		t.Errorf("expected IDE to remain ready after healthy check, got %s", store.instances["ws1"].IDE.Status)
	}
}

func TestSync_IDEBecameInactive(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	checker := &mockHTTPChecker{healthy: false, err: errors.New("connection refused")}
	adapter := &CodeServerAdapter{
		HTTPChecker: checker,
	}

	store := newMemStore()
	store.instances["ws1"] = &WorkspaceInstance{
		Spec:   WorkspaceSpec{Name: "ws1", ProjectRoot: projectRoot, Owner: "user", WorktreeName: "default", VCS: VCSTarget{Host: "github.com"}},
		Status: StatusReady,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Status:  StatusReady,
		},
	}

	s := NewSyncer(store, filepath.Join(dir, "logs")).WithIDE(adapter, nil)
	_, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}

	// IDE should now be pending (stopped)
	if store.instances["ws1"].IDE.Status != StatusPending {
		t.Errorf("expected IDE status pending after stop, got %s", store.instances["ws1"].IDE.Status)
	}

	// Should have emitted ide_stopped event in the IDE event stream
	ideEvents := store.instances["ws1"].IDE.Events
	if len(ideEvents) == 0 {
		t.Fatal("expected IDE events")
	}
	lastIDEEvent := ideEvents[len(ideEvents)-1]
	if lastIDEEvent.Event != IDEEventStopped {
		t.Errorf("expected ide_stopped event, got %s", lastIDEEvent.Event)
	}
}

func TestDeprovision_StopsIDE(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()
	checker := &mockHTTPChecker{healthy: true}
	portFile := filepath.Join(dir, "ports.json")

	adapter := &CodeServerAdapter{
		EnvDir:        filepath.Join(dir, "env"),
		SystemdRunner: runner,
		HTTPChecker:   checker,
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}
	pa := NewPortAllocator(portFile)

	// Pre-allocate a port
	key := PortKey("user", "feature")
	pa.Allocate(key)

	store := newMemStore()
	store.instances["myrepo/feature"] = &WorkspaceInstance{
		Spec: WorkspaceSpec{
			Name:         "myrepo/feature",
			IsDefault:    false,
			WorktreeName: "feature",
			ProjectRoot:  filepath.Join(dir, "repo", ".worktrees", "feature"),
			RepoRoot:     filepath.Join(dir, "repo"),
			BareRoot:     filepath.Join(dir, "repo", ".bare"),
			Owner:        "user",
		},
		Status: StatusReady,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Status:  StatusReady,
		},
	}

	p := &Provisioner{
		LogDir:        filepath.Join(dir, "logs"),
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	// Create fake worktree dir so deprovision proceeds (will fail at git remove, which is expected for unit test)
	os.MkdirAll(filepath.Join(dir, "repo", ".worktrees", "feature", ".git"), 0755)

	// The deprovision will fail at git worktree remove (no real git repo), but
	// IDE stop should have been called before that
	p.Deprovision(store, "myrepo/feature", true)

	// Verify IDE was stopped
	if len(runner.stopped) != 1 {
		t.Errorf("expected 1 unit stopped, got %d", len(runner.stopped))
	}

	// Verify port was released
	_, found := pa.Lookup(key)
	if found {
		t.Error("expected port to be released after deprovision")
	}
}

func TestStopIDE_PreservesInstance(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()
	portFile := filepath.Join(dir, "ports.json")

	adapter := &CodeServerAdapter{
		EnvDir:        filepath.Join(dir, "env"),
		SystemdRunner: runner,
		HTTPChecker:   &mockHTTPChecker{},
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}
	pa := NewPortAllocator(portFile)
	key := PortKey("user", "default")
	pa.Allocate(key)

	inst := &WorkspaceInstance{
		Spec: WorkspaceSpec{
			Name:         "myrepo",
			WorktreeName: "default",
			ProjectRoot:  filepath.Join(dir, "repo", "default"),
			Owner:        "user",
		},
		Status: StatusReady,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Events: []IDEEventRecord{
				{Event: IDEEventStarted, Timestamp: time.Now()},
				{Event: IDEEventReady, Timestamp: time.Now()},
			},
			Status: StatusReady,
		},
	}

	p := &Provisioner{
		LogDir:        filepath.Join(dir, "logs"),
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	p.stopIDE(inst, inst.Spec)

	// IDE instance must be preserved (not nil)
	if inst.IDE == nil {
		t.Fatal("expected IDE instance to be preserved after stop, got nil")
	}

	// Status must be pending after stop
	if inst.IDE.Status != StatusPending {
		t.Errorf("expected IDE status %q after stop, got %q", StatusPending, inst.IDE.Status)
	}

	// Must have ide_stopped event in the trail
	hasStopped := false
	for _, ev := range inst.IDE.Events {
		if ev.Event == IDEEventStopped {
			hasStopped = true
		}
	}
	if !hasStopped {
		t.Error("expected ide_stopped event in IDE event trail")
	}

	// Event trail should have 3 events: started, ready, stopped
	if len(inst.IDE.Events) != 3 {
		t.Errorf("expected 3 IDE events, got %d", len(inst.IDE.Events))
	}
}

func TestWorkspaceEventsDoNotContainIDEEvents(t *testing.T) {
	dir := t.TempDir()
	runner := newMockSystemdRunner()
	checker := &mockHTTPChecker{healthy: true}
	portFile := filepath.Join(dir, "ports.json")

	adapter := &CodeServerAdapter{
		EnvDir:        filepath.Join(dir, "env"),
		SystemdRunner: runner,
		HTTPChecker:   checker,
		PollTimeout:   1 * time.Second,
		PollInterval:  10 * time.Millisecond,
	}
	pa := NewPortAllocator(portFile)

	store := newMemStore()
	projectRoot := filepath.Join(dir, "repo", "default")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "fake", Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     filepath.Join(dir, "repo"),
		BareRoot:     filepath.Join(dir, "repo", ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		IDE:          &IDESpecConfig{Adapter: "openvscode-server"},
	}

	p := &Provisioner{
		LogDir:        filepath.Join(dir, "logs"),
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// Workspace event stream must NOT contain any ide_* events
	for _, ev := range inst.Events {
		evStr := string(ev.Event)
		if strings.HasPrefix(evStr, "ide_") {
			t.Errorf("workspace event stream contains IDE event %q — IDE events should only be on inst.IDE.Events", evStr)
		}
	}

	// IDE event stream must contain ide_started and ide_ready
	if inst.IDE == nil {
		t.Fatal("expected IDE instance to be set")
	}
	hasStarted, hasReady := false, false
	for _, ev := range inst.IDE.Events {
		if ev.Event == IDEEventStarted {
			hasStarted = true
		}
		if ev.Event == IDEEventReady {
			hasReady = true
		}
	}
	if !hasStarted {
		t.Error("expected ide_started event in IDE event stream")
	}
	if !hasReady {
		t.Error("expected ide_ready event in IDE event stream")
	}
}

func TestCredentialCheckEmitsGitCredentialsExistEvent(t *testing.T) {
	// This test validates that git_credentials_exist events appear in the
	// workspace event stream (inst.Events), not in the IDE event stream.
	// The checkCredentials function reads from a fixed path under /home/<owner>/,
	// so we set up the credential file there.
	owner := currentUser()
	credDir := filepath.Join("/home", owner, ".config/dsc/credentials")
	if err := os.MkdirAll(credDir, 0755); err != nil {
		t.Skipf("cannot create credential dir at %s (CI without /home): %v", credDir, err)
	}
	credFile := filepath.Join(credDir, "git-credentials")
	if err := os.WriteFile(credFile, []byte("https://x-access-token:tok@github.com\n"), 0644); err != nil {
		t.Skipf("cannot write credential file: %v", err)
	}
	defer os.Remove(credFile)

	dir := t.TempDir()
	store := newMemStore()
	projectRoot := filepath.Join(dir, "repo", "default")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "fake", Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     filepath.Join(dir, "repo"),
		BareRoot:     filepath.Join(dir, "repo", ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        owner,
	}

	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// Check for git_credentials_exist event in workspace event stream
	hasCredEvent := false
	for _, ev := range inst.Events {
		if ev.Event == EventGitCredentialsExist {
			hasCredEvent = true
		}
	}
	if !hasCredEvent {
		t.Error("expected git_credentials_exist event in workspace event stream")
	}
}

func TestIDESpecConfig_JSONRoundTrip(t *testing.T) {
	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", Branch: "main"},
		ProjectRoot:  "/tmp/repo/default",
		RepoRoot:     "/tmp/repo",
		BareRoot:     "/tmp/repo/.bare",
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        "user",
		IDE:          &IDESpecConfig{Adapter: "openvscode-server"},
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if !strings.Contains(string(data), `"adapter":"openvscode-server"`) {
		t.Error("expected IDE adapter in JSON")
	}

	var got WorkspaceSpec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.IDE == nil {
		t.Fatal("expected IDE to be set after round-trip")
	}
	if got.IDE.Adapter != "openvscode-server" {
		t.Errorf("expected adapter 'openvscode-server', got %q", got.IDE.Adapter)
	}
}

func TestIDESpecConfig_OmittedWhenNil(t *testing.T) {
	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", Branch: "main"},
		ProjectRoot:  "/tmp/repo/default",
		RepoRoot:     "/tmp/repo",
		BareRoot:     "/tmp/repo/.bare",
		WorktreeName: "default",
		Owner:        "user",
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if strings.Contains(string(data), `"ide"`) {
		t.Error("expected ide to be omitted from JSON when nil")
	}
}

func TestEnvFileContent(t *testing.T) {
	ctx := IDEContext{
		Owner:        "jperez9315",
		Port:         9100,
		WorktreePath: "/home/jperez9315/code/gitlab.com/org/ocr-service/default",
		WorktreeName: "default",
	}
	got := envFileContent(ctx)
	want := fmt.Sprintf("IDE_OWNER=jperez9315\nIDE_PORT=9100\nIDE_WORKTREE_PATH=/home/jperez9315/code/gitlab.com/org/ocr-service/default\n")
	if got != want {
		t.Errorf("env content mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

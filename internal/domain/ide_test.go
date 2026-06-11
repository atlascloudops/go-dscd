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

func TestUnitName_SlashInWorktreeName(t *testing.T) {
	ctx := IDEContext{Owner: "alice", WorktreeName: "feat/bar"}
	got := UnitName(ctx)
	want := "openvscode-server@alice--feat--bar.service"
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
	scope := EventScope{Kind: ScopeKindIDE, Name: "infra"}
	instance := IDEInstance{
		Name:    "infra",
		Adapter: "openvscode-server",
		Port:    9100,
		Events: []EventRecord{
			{Scope: scope, Event: string(IDEEventStarted), Timestamp: time.Now().UTC().Truncate(time.Second), Detail: "port=9100"},
			{Scope: scope, Event: string(IDEEventReady), Timestamp: time.Now().UTC().Truncate(time.Second), Detail: "port=9100"},
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

	if got.Name != instance.Name {
		t.Errorf("name: expected %q, got %q", instance.Name, got.Name)
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
	if got.Events[0].Scope.Kind != ScopeKindIDE || got.Events[0].Scope.Name != "infra" {
		t.Errorf("event scope: expected ide:infra, got %s", got.Events[0].Scope)
	}
	if got.Status != StatusReady {
		t.Errorf("status: expected %q, got %q", StatusReady, got.Status)
	}
}

func TestWorkspace_IDEInstanceJSON(t *testing.T) {
	scope := EventScope{Kind: ScopeKindIDE, Name: "myrepo"}
	inst := Workspace{
		Name:   "myrepo",
		Status: StatusPending,
		IDE: map[string]*IDEInstance{
			"default": {
				Name:    "myrepo",
				Adapter: "openvscode-server",
				Port:    9100,
				Events: []EventRecord{
					{Scope: scope, Event: string(IDEEventStarted), Timestamp: time.Now().UTC().Truncate(time.Second)},
					{Scope: scope, Event: string(IDEEventReady), Timestamp: time.Now().UTC().Truncate(time.Second)},
				},
				Status: StatusReady,
			},
		},
	}

	data, err := json.Marshal(inst)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Workspace
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.IDE == nil {
		t.Fatal("IDE should not be nil after round-trip")
	}
	ide := got.IDE["default"]
	if ide == nil {
		t.Fatal("IDE[default] should not be nil after round-trip")
	}
	if ide.Name != "myrepo" {
		t.Errorf("IDE.Name: expected myrepo, got %q", ide.Name)
	}
	if ide.Port != 9100 {
		t.Errorf("IDE.Port: expected 9100, got %d", ide.Port)
	}
	if ide.Adapter != "openvscode-server" {
		t.Errorf("IDE.Adapter: expected openvscode-server, got %q", ide.Adapter)
	}
	if ide.Status != StatusReady {
		t.Errorf("IDE.Status: expected %q, got %q", StatusReady, ide.Status)
	}
	if len(ide.Events) != 2 {
		t.Errorf("IDE.Events: expected 2, got %d", len(ide.Events))
	}
}

func TestWorkspace_IDEInstanceOmittedWhenNil(t *testing.T) {
	inst := Workspace{
		Name:   "myrepo",
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

// --- IDEInstance.RecordEvent Tests ---

func TestIDEInstance_RecordEvent_AppendsWithCorrectScope(t *testing.T) {
	ide := &IDEInstance{
		Name:    "infra",
		Adapter: "openvscode-server",
		Port:    9100,
	}

	ide.RecordEvent(IDEEventStarted, "port=9100")

	if len(ide.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(ide.Events))
	}

	ev := ide.Events[0]
	if ev.Scope.Kind != ScopeKindIDE {
		t.Errorf("scope kind: expected %q, got %q", ScopeKindIDE, ev.Scope.Kind)
	}
	if ev.Scope.Name != "infra" {
		t.Errorf("scope name: expected %q, got %q", "infra", ev.Scope.Name)
	}
	if ev.Scope.String() != "ide:infra" {
		t.Errorf("scope string: expected %q, got %q", "ide:infra", ev.Scope.String())
	}
	if ev.Event != string(IDEEventStarted) {
		t.Errorf("event: expected %q, got %q", IDEEventStarted, ev.Event)
	}
	if ev.Detail != "port=9100" {
		t.Errorf("detail: expected %q, got %q", "port=9100", ev.Detail)
	}
	if ev.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestIDEInstance_RecordEvent_ProjectsStatus(t *testing.T) {
	ide := &IDEInstance{
		Name:    "infra",
		Adapter: "openvscode-server",
		Port:    9100,
	}

	// Started -> Provisioning
	ide.RecordEvent(IDEEventStarted, "port=9100")
	if ide.Status != StatusProvisioning {
		t.Errorf("after started: expected %q, got %q", StatusProvisioning, ide.Status)
	}

	// Ready -> Ready
	ide.RecordEvent(IDEEventReady, "port=9100")
	if ide.Status != StatusReady {
		t.Errorf("after ready: expected %q, got %q", StatusReady, ide.Status)
	}

	// Stopped -> Pending
	ide.RecordEvent(IDEEventStopped, "port=9100")
	if ide.Status != StatusPending {
		t.Errorf("after stopped: expected %q, got %q", StatusPending, ide.Status)
	}

	if len(ide.Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(ide.Events))
	}
}

func TestIDEInstance_RecordEvent_FailedStatus(t *testing.T) {
	ide := &IDEInstance{
		Name:    "infra/feat",
		Adapter: "openvscode-server",
		Port:    9100,
	}

	ide.RecordEvent(IDEEventStarted, "port=9100")
	ide.RecordEvent(IDEEventFailed, "systemd error")

	if ide.Status != StatusFailed {
		t.Errorf("expected %q, got %q", StatusFailed, ide.Status)
	}

	// Scope should use workspace name with slash
	if ide.Events[0].Scope.String() != "ide:infra/feat" {
		t.Errorf("scope: expected %q, got %q", "ide:infra/feat", ide.Events[0].Scope.String())
	}
}

func TestIDEInstance_RecordEvent_JSONRoundTrip(t *testing.T) {
	ide := &IDEInstance{
		Name:    "infra",
		Adapter: "openvscode-server",
		Port:    9100,
	}

	ide.RecordEvent(IDEEventStarted, "port=9100")
	ide.RecordEvent(IDEEventReady, "port=9100")

	data, err := json.Marshal(ide)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got IDEInstance
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Name != "infra" {
		t.Errorf("name: expected %q, got %q", "infra", got.Name)
	}
	if len(got.Events) != 2 {
		t.Fatalf("events: expected 2, got %d", len(got.Events))
	}
	if got.Events[0].Scope.String() != "ide:infra" {
		t.Errorf("scope: expected %q, got %q", "ide:infra", got.Events[0].Scope.String())
	}
	if got.Events[0].Event != string(IDEEventStarted) {
		t.Errorf("event[0]: expected %q, got %q", IDEEventStarted, got.Events[0].Event)
	}
	if got.Events[1].Event != string(IDEEventReady) {
		t.Errorf("event[1]: expected %q, got %q", IDEEventReady, got.Events[1].Event)
	}
	if got.Status != StatusReady {
		t.Errorf("status: expected %q, got %q", StatusReady, got.Status)
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

	// Create a fake worktree on disk at the derived path
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: "fake"},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	projectRoot := params.ProjectRoot()
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// Provision does NOT start IDE — IDE startup is separate (via StartIDE).
	if inst.IDE != nil && len(inst.IDE) > 0 {
		t.Fatal("expected no IDE instances during provision (IDE startup is separate)")
	}

	// Workspace status should still be Ready
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
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: "fake"},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	projectRoot := params.ProjectRoot()
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision should succeed: %v", err)
	}

	// IDE startup is deferred to the ide-worktree-scoping story.
	// Provision should NOT start IDE even when adapter would fail.
	if inst.IDE != nil && len(inst.IDE) > 0 {
		t.Fatal("expected no IDE instances during provision (IDE startup is separate)")
	}

	// Workspace status should still be Ready
	if inst.Status != StatusReady {
		t.Errorf("expected status ready, got %s", inst.Status)
	}
}

func TestProvision_WithoutIDE_SkipsIDEPhase(t *testing.T) {
	dir := t.TempDir()
	store := newMemStore()
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: "fake"},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	projectRoot := params.ProjectRoot()
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	p := &Provisioner{}

	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	if inst.IDE != nil && len(inst.IDE) > 0 {
		t.Error("expected no IDE instance when IDE not requested")
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
	store.instances["ws1"] = &Workspace{
		Name:   "ws1",
		Owner:  "user",
		Repo:   RepoInfo{Host: "github.com"},
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: projectRoot, IsDefault: true},
		},
		IDE: map[string]*IDEInstance{
			"default": {
				Name:    "ws1",
				Adapter: "openvscode-server",
				Port:    9100,
				Status:  StatusReady,
			},
		},
	}

	s := NewSyncer(store, nil).WithIDE(adapter, nil)
	_, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}

	// IDE should still be ready
	if store.instances["ws1"].IDE["default"].Status != StatusReady {
		t.Errorf("expected IDE to remain ready after healthy check, got %s", store.instances["ws1"].IDE["default"].Status)
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
	store.instances["ws1"] = &Workspace{
		Name:   "ws1",
		Owner:  "user",
		Repo:   RepoInfo{Host: "github.com"},
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: projectRoot, IsDefault: true},
		},
		IDE: map[string]*IDEInstance{
			"default": {
				Name:    "ws1",
				Adapter: "openvscode-server",
				Port:    9100,
				Status:  StatusReady,
			},
		},
	}

	s := NewSyncer(store, nil).WithIDE(adapter, nil)
	_, err := s.Sync()
	if err != nil {
		t.Fatal(err)
	}

	// IDE should now be pending (stopped)
	if store.instances["ws1"].IDE["default"].Status != StatusPending {
		t.Errorf("expected IDE status pending after stop, got %s", store.instances["ws1"].IDE["default"].Status)
	}

	// Should have emitted ide_stopped event in the IDE event stream
	ideEvents := store.instances["ws1"].IDE["default"].Events
	if len(ideEvents) == 0 {
		t.Fatal("expected IDE events")
	}
	lastIDEEvent := ideEvents[len(ideEvents)-1]
	if lastIDEEvent.Event != string(IDEEventStopped) {
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
	store.instances["myrepo"] = &Workspace{
		Name:     "myrepo",
		Owner:    "user",
		Status:   StatusReady,
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: filepath.Join(dir, "repo", "default"), IsDefault: true},
			{Name: "feature", ProjectRoot: filepath.Join(dir, "repo", ".worktrees", "feature"), IsDefault: false},
		},
		IDE: map[string]*IDEInstance{
			"feature": {
				Name:    "myrepo",
				Adapter: "openvscode-server",
				Port:    9100,
				Status:  StatusReady,
			},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	// Create fake worktree dir so deprovision proceeds
	os.MkdirAll(filepath.Join(dir, "repo", ".worktrees", "feature", ".git"), 0755)
	os.MkdirAll(filepath.Join(dir, "repo", "default", ".git"), 0755)

	p.Deprovision(store, "myrepo", true)

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

	ideScope := EventScope{Kind: ScopeKindIDE, Name: "myrepo"}
	inst := &Workspace{
		Name:     "myrepo",
		Owner:    "user",
		Status:   StatusReady,
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: filepath.Join(dir, "repo", "default"), IsDefault: true},
		},
		IDE: map[string]*IDEInstance{
			"default": {
				Name:    "myrepo",
				Adapter: "openvscode-server",
				Port:    9100,
				Events: []EventRecord{
					{Scope: ideScope, Event: string(IDEEventStarted), Timestamp: time.Now()},
					{Scope: ideScope, Event: string(IDEEventReady), Timestamp: time.Now()},
				},
				Status: StatusReady,
			},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	p.stopIDEForWorktree(inst, "default", filepath.Join(dir, "repo", "default"))

	// IDE instance must be preserved (not nil)
	if inst.IDE == nil || inst.IDE["default"] == nil {
		t.Fatal("expected IDE instance to be preserved after stop, got nil")
	}

	ide := inst.IDE["default"]

	// Status must be pending after stop
	if ide.Status != StatusPending {
		t.Errorf("expected IDE status %q after stop, got %q", StatusPending, ide.Status)
	}

	// Must have ide_stopped event in the trail
	hasStopped := false
	for _, ev := range ide.Events {
		if ev.Event == string(IDEEventStopped) {
			hasStopped = true
		}
	}
	if !hasStopped {
		t.Error("expected ide_stopped event in IDE event trail")
	}

	// Event trail should have 3 events: started, ready, stopped
	if len(ide.Events) != 3 {
		t.Errorf("expected 3 IDE events, got %d", len(ide.Events))
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
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: "fake"},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	projectRoot := params.ProjectRoot()
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	inst, err := p.Provision(store, params)
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

	// IDE startup is deferred — no IDE instances should exist on provision
	if inst.IDE != nil && len(inst.IDE) > 0 {
		t.Fatal("expected no IDE instances during provision (IDE startup is separate)")
	}
}

// --- StartIDE / StopIDE public API tests ---

func TestStartIDE_StartsAndPersists(t *testing.T) {
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

	store.instances["myrepo"] = &Workspace{
		Name:  "myrepo",
		Owner: "user",
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: projectRoot, IsDefault: true},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	result, err := p.StartIDE(store, "myrepo", "default")
	if err != nil {
		t.Fatalf("StartIDE: %v", err)
	}

	if result.WorkspaceName != "myrepo" {
		t.Errorf("expected workspace 'myrepo', got %q", result.WorkspaceName)
	}
	if result.WorktreeName != "default" {
		t.Errorf("expected worktree 'default', got %q", result.WorktreeName)
	}
	if result.Adapter != "openvscode-server" {
		t.Errorf("expected adapter 'openvscode-server', got %q", result.Adapter)
	}
	if result.Port < 9100 || result.Port > 9199 {
		t.Errorf("expected port in range 9100-9199, got %d", result.Port)
	}
	if result.Status != string(StatusReady) {
		t.Errorf("expected status 'ready', got %q", result.Status)
	}

	// Verify IDE was persisted in state
	ws := store.instances["myrepo"]
	ide := ws.IDEForWorktree("default")
	if ide == nil {
		t.Fatal("expected IDE instance persisted in state")
	}
	if ide.Port != result.Port {
		t.Errorf("persisted port %d != result port %d", ide.Port, result.Port)
	}
}

func TestStartIDE_Idempotent(t *testing.T) {
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

	store.instances["myrepo"] = &Workspace{
		Name:  "myrepo",
		Owner: "user",
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: projectRoot, IsDefault: true},
		},
		IDE: map[string]*IDEInstance{
			"default": {
				Name:    "myrepo",
				Adapter: "openvscode-server",
				Port:    9100,
				Status:  StatusReady,
			},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	result, err := p.StartIDE(store, "myrepo", "default")
	if err != nil {
		t.Fatalf("StartIDE: %v", err)
	}

	// Should return existing state without restarting
	if result.Port != 9100 {
		t.Errorf("expected existing port 9100, got %d", result.Port)
	}
	if result.Status != string(StatusReady) {
		t.Errorf("expected status 'ready', got %q", result.Status)
	}
	// Systemd should NOT have been called (idempotent path)
	if len(runner.started) != 0 {
		t.Errorf("expected no systemd starts on idempotent call, got %d", len(runner.started))
	}
}

func TestStartIDE_DefaultWorktreeWhenEmpty(t *testing.T) {
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

	store.instances["myrepo"] = &Workspace{
		Name:  "myrepo",
		Owner: "user",
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: projectRoot, IsDefault: true},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	// Empty worktree name should default to "default"
	result, err := p.StartIDE(store, "myrepo", "")
	if err != nil {
		t.Fatalf("StartIDE: %v", err)
	}
	if result.WorktreeName != "default" {
		t.Errorf("expected worktree 'default', got %q", result.WorktreeName)
	}
}

func TestStartIDE_WorkspaceNotFound(t *testing.T) {
	dir := t.TempDir()
	portFile := filepath.Join(dir, "ports.json")
	adapter := &CodeServerAdapter{}
	pa := NewPortAllocator(portFile)

	store := newMemStore()

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	_, err := p.StartIDE(store, "nonexistent", "default")
	if err == nil {
		t.Fatal("expected error for nonexistent workspace")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrNotFound {
		t.Errorf("expected NOT_FOUND, got %q", pe.Code)
	}
}

func TestStartIDE_WorktreeNotFound(t *testing.T) {
	dir := t.TempDir()
	portFile := filepath.Join(dir, "ports.json")
	adapter := &CodeServerAdapter{}
	pa := NewPortAllocator(portFile)

	store := newMemStore()
	store.instances["myrepo"] = &Workspace{
		Name:  "myrepo",
		Owner: "user",
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: "/tmp/default", IsDefault: true},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	_, err := p.StartIDE(store, "myrepo", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent worktree")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrNotFound {
		t.Errorf("expected NOT_FOUND, got %q", pe.Code)
	}
}

func TestStartIDE_NotConfigured(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{} // no adapter or allocator

	_, err := p.StartIDE(store, "myrepo", "default")
	if err == nil {
		t.Fatal("expected error when IDE not configured")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != "IDE_NOT_CONFIGURED" {
		t.Errorf("expected IDE_NOT_CONFIGURED, got %q", pe.Code)
	}
}

func TestStopIDE_StopsAndPersists(t *testing.T) {
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

	// Pre-allocate a port
	key := PortKey("user", "default")
	pa.Allocate(key)

	store := newMemStore()
	projectRoot := filepath.Join(dir, "repo", "default")
	store.instances["myrepo"] = &Workspace{
		Name:  "myrepo",
		Owner: "user",
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: projectRoot, IsDefault: true},
		},
		IDE: map[string]*IDEInstance{
			"default": {
				Name:    "myrepo",
				Adapter: "openvscode-server",
				Port:    9100,
				Status:  StatusReady,
			},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	result, err := p.StopIDE(store, "myrepo", "default")
	if err != nil {
		t.Fatalf("StopIDE: %v", err)
	}

	if result.WorkspaceName != "myrepo" {
		t.Errorf("expected workspace 'myrepo', got %q", result.WorkspaceName)
	}
	if result.WorktreeName != "default" {
		t.Errorf("expected worktree 'default', got %q", result.WorktreeName)
	}

	// Verify systemd unit was stopped
	if len(runner.stopped) != 1 {
		t.Errorf("expected 1 unit stopped, got %d", len(runner.stopped))
	}

	// Verify port was released
	_, found := pa.Lookup(key)
	if found {
		t.Error("expected port to be released after StopIDE")
	}
}

func TestStopIDE_NoIDEInstance(t *testing.T) {
	dir := t.TempDir()
	portFile := filepath.Join(dir, "ports.json")
	adapter := &CodeServerAdapter{}
	pa := NewPortAllocator(portFile)

	store := newMemStore()
	store.instances["myrepo"] = &Workspace{
		Name:  "myrepo",
		Owner: "user",
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: "/tmp/default", IsDefault: true},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	_, err := p.StopIDE(store, "myrepo", "default")
	if err == nil {
		t.Fatal("expected error when no IDE instance exists")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrNotFound {
		t.Errorf("expected NOT_FOUND, got %q", pe.Code)
	}
}

func TestStartIDE_BranchWorktree(t *testing.T) {
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
	defaultRoot := filepath.Join(dir, "repo", "default")
	featureRoot := filepath.Join(dir, "repo", ".worktrees", "feat-bar")
	os.MkdirAll(filepath.Join(defaultRoot, ".git"), 0755)
	os.MkdirAll(filepath.Join(featureRoot, ".git"), 0755)

	store.instances["myrepo"] = &Workspace{
		Name:  "myrepo",
		Owner: "user",
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: defaultRoot, IsDefault: true},
			{Name: "feat/bar", ProjectRoot: featureRoot, IsDefault: false, Branch: "feat/bar"},
		},
	}

	p := &Provisioner{
		IDEAdapter:    adapter,
		PortAllocator: pa,
	}

	result, err := p.StartIDE(store, "myrepo", "feat/bar")
	if err != nil {
		t.Fatalf("StartIDE: %v", err)
	}

	if result.WorktreeName != "feat/bar" {
		t.Errorf("expected worktree 'feat/bar', got %q", result.WorktreeName)
	}
	if result.Status != string(StatusReady) {
		t.Errorf("expected status 'ready', got %q", result.Status)
	}

	// Verify the IDE was stored under the correct worktree key
	ws := store.instances["myrepo"]
	if ws.IDE == nil || ws.IDE["feat/bar"] == nil {
		t.Fatal("expected IDE instance stored under 'feat/bar' key")
	}

	// Verify the systemd unit name uses the sanitized worktree name
	if len(runner.started) != 1 {
		t.Fatalf("expected 1 unit started, got %d", len(runner.started))
	}
	expectedUnit := "openvscode-server@user--feat--bar.service"
	if runner.started[0] != expectedUnit {
		t.Errorf("expected unit %q, got %q", expectedUnit, runner.started[0])
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

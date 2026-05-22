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

func TestIDEState_JSONRoundTrip(t *testing.T) {
	state := IDEState{
		AdapterName: "openvscode-server",
		Port:        9100,
		Active:      true,
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got IDEState
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.AdapterName != state.AdapterName {
		t.Errorf("adapter_name: expected %q, got %q", state.AdapterName, got.AdapterName)
	}
	if got.Port != state.Port {
		t.Errorf("port: expected %d, got %d", state.Port, got.Port)
	}
	if got.Active != state.Active {
		t.Errorf("active: expected %v, got %v", state.Active, got.Active)
	}
}

func TestWorkspaceInstance_IDEStateJSON(t *testing.T) {
	inst := WorkspaceInstance{
		State: StatePending,
		IDE: &IDEState{
			AdapterName: "openvscode-server",
			Port:        9100,
			Active:      true,
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
	if got.IDE.AdapterName != "openvscode-server" {
		t.Errorf("IDE.AdapterName: expected openvscode-server, got %q", got.IDE.AdapterName)
	}
}

func TestWorkspaceInstance_IDEStateOmittedWhenNil(t *testing.T) {
	inst := WorkspaceInstance{
		State: StatePending,
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

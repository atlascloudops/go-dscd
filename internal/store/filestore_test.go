package store

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewFileStore(path)

	now := time.Now().UTC().Truncate(time.Second)
	errMsg := "test error"
	instances := map[string]*domain.Workspace{
		"ws1": {
			Spec: domain.WorkspaceSpec{
				Name: "ws1",
				VCS: domain.VCSTarget{
					Host:     "github.com",
					Repo:     "org/repo1",
					Branch:   "main",
					CloneURL: "https://github.com/org/repo1.git",
				},
				PatName:     "gh-token",
				ProjectRoot: "/home/user/code/repo1",
				Owner:       "user",
			},
			Status:      domain.StatusReady,
			ProvisionedAt:  &now,
			LastError:      nil,
		},
		"ws2": {
			Spec: domain.WorkspaceSpec{
				Name: "ws2",
				VCS: domain.VCSTarget{
					Host:     "gitlab.com",
					Repo:     "org/repo2",
					Branch:   "dev",
					CloneURL: "https://gitlab.com/org/repo2.git",
				},
				PatName:     "gl-token",
				ProjectRoot: "/home/user/code/repo2",
				Owner:       "user",
			},
			Status:      domain.StatusFailed,
			ProvisionedAt:  &now,
			LastError:      &errMsg,
		},
	}

	if err := s.Save(instances); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(loaded))
	}

	ws1 := loaded["ws1"]
	if ws1.Spec.Name != "ws1" || ws1.Status != domain.StatusReady {
		t.Fatalf("ws1 mismatch: %+v", ws1)
	}
	if ws1.ProvisionedAt == nil || !ws1.ProvisionedAt.Equal(now) {
		t.Fatalf("ws1 provisioned_at mismatch: got %v, want %v", ws1.ProvisionedAt, now)
	}

	ws2 := loaded["ws2"]
	if ws2.Spec.Name != "ws2" || ws2.Status != domain.StatusFailed {
		t.Fatalf("ws2 mismatch: %+v", ws2)
	}
	if ws2.LastError == nil || *ws2.LastError != errMsg {
		t.Fatalf("ws2 last_error mismatch: %v", ws2.LastError)
	}
}

func TestRoundTrip_IDEInstance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewFileStore(path)

	ts := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	instances := map[string]*domain.Workspace{
		"ws-ide": {
			Spec: domain.WorkspaceSpec{
				Name: "ws-ide",
				VCS: domain.VCSTarget{
					Host:     "github.com",
					Repo:     "org/repo1",
					Branch:   "main",
					CloneURL: "https://github.com/org/repo1.git",
				},
				ProjectRoot:  "/home/user/code/repo1/default",
				RepoRoot:     "/home/user/code/repo1",
				BareRoot:     "/home/user/code/repo1/.bare",
				WorktreeName: "default",
				Owner:        "user",
				IDE:          &domain.IDESpecConfig{Adapter: "openvscode-server"},
			},
			Status:         domain.StatusReady,
			IDE: &domain.IDEInstance{
				Name:    "ws-ide",
				Adapter: "openvscode-server",
				Port:    9100,
				Events: []domain.EventRecord{
					{Scope: domain.EventScope{Kind: domain.ScopeKindIDE, Name: "ws-ide"}, Event: string(domain.IDEEventStarted), Timestamp: ts, Detail: "port=9100"},
					{Scope: domain.EventScope{Kind: domain.ScopeKindIDE, Name: "ws-ide"}, Event: string(domain.IDEEventReady), Timestamp: ts, Detail: "port=9100"},
				},
				Status: domain.StatusReady,
			},
		},
	}

	if err := s.Save(instances); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}

	ws := loaded["ws-ide"]
	if ws == nil {
		t.Fatal("expected ws-ide in loaded instances")
	}
	if ws.IDE == nil {
		t.Fatal("expected IDE instance after round-trip")
	}
	if ws.IDE.Adapter != "openvscode-server" {
		t.Errorf("IDE.Adapter: expected openvscode-server, got %q", ws.IDE.Adapter)
	}
	if ws.IDE.Port != 9100 {
		t.Errorf("IDE.Port: expected 9100, got %d", ws.IDE.Port)
	}
	if len(ws.IDE.Events) != 2 {
		t.Fatalf("IDE.Events: expected 2, got %d", len(ws.IDE.Events))
	}
	if ws.IDE.Events[0].Event != string(domain.IDEEventStarted) {
		t.Errorf("IDE.Events[0]: expected %q, got %q", domain.IDEEventStarted, ws.IDE.Events[0].Event)
	}
	if ws.IDE.Events[1].Event != string(domain.IDEEventReady) {
		t.Errorf("IDE.Events[1]: expected %q, got %q", domain.IDEEventReady, ws.IDE.Events[1].Event)
	}
	if ws.IDE.Name != "ws-ide" {
		t.Errorf("IDE.Name: expected %q, got %q", "ws-ide", ws.IDE.Name)
	}
	if ws.IDE.Status != domain.StatusReady {
		t.Errorf("IDE.Status: expected %q, got %q", domain.StatusReady, ws.IDE.Status)
	}
}

func TestLoadNonexistent(t *testing.T) {
	s := NewFileStore(filepath.Join(t.TempDir(), "missing.json"))
	instances, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(instances))
	}
}

func TestRoundTrip_Credentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewFileStore(path)

	now := time.Now().UTC().Truncate(time.Second)
	state := &domain.DaemonState{
		Workspaces: map[string]*domain.Workspace{
			"ws1": {
				Spec: domain.WorkspaceSpec{
					Name: "ws1",
					VCS: domain.VCSTarget{
						Host:   "github.com",
						Repo:   "org/repo1",
						Branch: "main",
					},
					Owner: "jperez",
				},
				Status: domain.StatusReady,
			},
		},
		Credentials: map[string]*domain.CredentialState{
			"jperez": {
				Owner:        "jperez",
				GitHosts:     []string{"github.com", "gitlab.com"},
				SsoSession:   "dsc-session",
				LastSyncedAt: &now,
			},
		},
	}

	// Record an event on the credential state
	state.Credentials["jperez"].RecordEvent(domain.CredEventGitWritten, "github.com, gitlab.com")

	if err := s.SaveState(state); err != nil {
		t.Fatal(err)
	}

	loaded, err := s.LoadState()
	if err != nil {
		t.Fatal(err)
	}

	// Verify workspaces survived
	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Workspaces))
	}

	// Verify credentials
	if len(loaded.Credentials) != 1 {
		t.Fatalf("expected 1 credential entry, got %d", len(loaded.Credentials))
	}

	cs := loaded.Credentials["jperez"]
	if cs == nil {
		t.Fatal("expected jperez credential state")
	}
	if cs.Owner != "jperez" {
		t.Errorf("owner: expected %q, got %q", "jperez", cs.Owner)
	}
	if len(cs.GitHosts) != 2 {
		t.Fatalf("git_hosts: expected 2, got %d", len(cs.GitHosts))
	}
	if cs.SsoSession != "dsc-session" {
		t.Errorf("sso_session: expected %q, got %q", "dsc-session", cs.SsoSession)
	}
	if cs.LastSyncedAt == nil || !cs.LastSyncedAt.Equal(now) {
		t.Errorf("last_synced_at mismatch: got %v, want %v", cs.LastSyncedAt, now)
	}

	// Verify event
	if len(cs.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cs.Events))
	}
	evt := cs.Events[0]
	if evt.Scope.Kind != domain.ScopeKindCredentials || evt.Scope.Name != "jperez" {
		t.Errorf("event scope: expected credentials:jperez, got %s", evt.Scope.String())
	}
	if evt.Event != string(domain.CredEventGitWritten) {
		t.Errorf("event: expected %q, got %q", domain.CredEventGitWritten, evt.Event)
	}
}

func TestBackwardCompat_NoCredentialsKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write a state file without a credentials key (simulating pre-credentials format)
	legacy := `{
		"version": "v1",
		"updated_at": "2026-05-21T10:00:00Z",
		"workspaces": {
			"ws1": {
				"spec": {
					"name": "ws1",
					"vcs": {"host": "github.com", "repo": "org/repo1", "branch": "main"},
					"owner": "user"
				},
				"status": "ready"
			}
		}
	}`
	if err := os.WriteFile(path, []byte(legacy), 0664); err != nil {
		t.Fatal(err)
	}

	s := NewFileStore(path)
	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("loading legacy state without credentials key: %v", err)
	}

	if len(state.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(state.Workspaces))
	}
	if state.Credentials == nil {
		t.Fatal("expected non-nil credentials map")
	}
	if len(state.Credentials) != 0 {
		t.Fatalf("expected empty credentials map, got %d entries", len(state.Credentials))
	}
}

func TestSave_PreservesCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewFileStore(path)

	// Save state with credentials
	state := &domain.DaemonState{
		Workspaces: map[string]*domain.Workspace{},
		Credentials: map[string]*domain.CredentialState{
			"jperez": {
				Owner:    "jperez",
				GitHosts: []string{"github.com"},
			},
		},
	}
	if err := s.SaveState(state); err != nil {
		t.Fatal(err)
	}

	// Use workspace-only Save — credentials should be preserved
	if err := s.Save(map[string]*domain.Workspace{
		"ws1": {
			Spec: domain.WorkspaceSpec{Name: "ws1", Owner: "jperez"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Load full state and verify credentials survived
	loaded, err := s.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Credentials) != 1 {
		t.Fatalf("expected 1 credential entry after workspace-only save, got %d", len(loaded.Credentials))
	}
	if loaded.Credentials["jperez"] == nil {
		t.Fatal("expected jperez credential state to survive workspace-only save")
	}
	if len(loaded.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(loaded.Workspaces))
	}
}

func TestWithLockConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewFileStore(path)

	var counter int64
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.WithLock(func() error {
				val := atomic.LoadInt64(&counter)
				time.Sleep(time.Millisecond)
				atomic.StoreInt64(&counter, val+1)
				return nil
			})
			if err != nil {
				t.Errorf("lock error: %v", err)
			}
		}()
	}

	wg.Wait()

	if counter != 10 {
		t.Fatalf("expected counter=10, got %d (concurrent writes not serialized)", counter)
	}
}

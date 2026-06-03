package store

import (
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
	instances := map[string]*domain.WorkspaceInstance{
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
	instances := map[string]*domain.WorkspaceInstance{
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
				Adapter: "openvscode-server",
				Port:    9100,
				Events: []domain.IDEEventRecord{
					{Event: domain.IDEEventStarted, Timestamp: ts, Detail: "port=9100"},
					{Event: domain.IDEEventReady, Timestamp: ts, Detail: "port=9100"},
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
	if ws.IDE.Events[0].Event != domain.IDEEventStarted {
		t.Errorf("IDE.Events[0]: expected %q, got %q", domain.IDEEventStarted, ws.IDE.Events[0].Event)
	}
	if ws.IDE.Events[1].Event != domain.IDEEventReady {
		t.Errorf("IDE.Events[1]: expected %q, got %q", domain.IDEEventReady, ws.IDE.Events[1].Event)
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

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
			Lifecycle:      domain.LifecycleReady,
			CredentialHost: "github.com",
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
			Lifecycle:      domain.LifecycleFailed,
			CredentialHost: "gitlab.com",
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
	if ws1.Spec.Name != "ws1" || ws1.Lifecycle != domain.LifecycleReady {
		t.Fatalf("ws1 mismatch: %+v", ws1)
	}
	if ws1.ProvisionedAt == nil || !ws1.ProvisionedAt.Equal(now) {
		t.Fatalf("ws1 provisioned_at mismatch: got %v, want %v", ws1.ProvisionedAt, now)
	}

	ws2 := loaded["ws2"]
	if ws2.Spec.Name != "ws2" || ws2.Lifecycle != domain.LifecycleFailed {
		t.Fatalf("ws2 mismatch: %+v", ws2)
	}
	if ws2.LastError == nil || *ws2.LastError != errMsg {
		t.Fatalf("ws2 last_error mismatch: %v", ws2.LastError)
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

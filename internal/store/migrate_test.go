package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
)

// v1StateDefaultOnly is a v1 state file with a single default worktree entry.
const v1StateDefaultOnly = `{
  "version": "v1",
  "updated_at": "2026-06-01T10:00:00Z",
  "workspaces": {
    "infra": {
      "spec": {
        "name": "infra",
        "canonical_name": "infra",
        "vcs": {
          "host": "github.com",
          "auth_user": "bot",
          "repo": "org/infra",
          "branch": "main",
          "clone_url": "https://github.com/org/infra.git"
        },
        "pat_name": "gh-token",
        "project_root": "/home/ubuntu/code/github.com/org/infra/default",
        "repo_root": "/home/ubuntu/code/github.com/org/infra",
        "bare_root": "/home/ubuntu/code/github.com/org/infra/.bare",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu"
      },
      "events": [
        {"event": "clone_started", "timestamp": "2026-06-01T10:00:00Z", "detail": ""},
        {"event": "clone_completed", "timestamp": "2026-06-01T10:00:05Z", "detail": ""},
        {"event": "worktree_created", "timestamp": "2026-06-01T10:00:06Z", "detail": "default"}
      ],
      "status": "ready",
      "head_commit": "abc123",
      "provisioned_at": "2026-06-01T10:00:00Z"
    }
  }
}`

// v1StateWithBranch is a v1 state file with both a default and a branch worktree.
const v1StateWithBranch = `{
  "version": "v1",
  "updated_at": "2026-06-01T12:00:00Z",
  "workspaces": {
    "infra": {
      "spec": {
        "name": "infra",
        "canonical_name": "infra",
        "vcs": {
          "host": "github.com",
          "auth_user": "bot",
          "repo": "org/infra",
          "branch": "main",
          "clone_url": "https://github.com/org/infra.git"
        },
        "pat_name": "gh-token",
        "project_root": "/home/ubuntu/code/github.com/org/infra/default",
        "repo_root": "/home/ubuntu/code/github.com/org/infra",
        "bare_root": "/home/ubuntu/code/github.com/org/infra/.bare",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu"
      },
      "events": [
        {"event": "clone_started", "timestamp": "2026-06-01T10:00:00Z", "detail": ""},
        {"event": "clone_completed", "timestamp": "2026-06-01T10:00:05Z", "detail": ""},
        {"event": "worktree_created", "timestamp": "2026-06-01T10:00:06Z", "detail": "default"}
      ],
      "status": "ready",
      "head_commit": "abc123",
      "provisioned_at": "2026-06-01T10:00:00Z"
    },
    "infra/feat": {
      "spec": {
        "name": "infra",
        "canonical_name": "infra/feat",
        "vcs": {
          "host": "github.com",
          "auth_user": "bot",
          "repo": "org/infra",
          "branch": "feat",
          "clone_url": "https://github.com/org/infra.git"
        },
        "pat_name": "gh-token",
        "project_root": "/home/ubuntu/code/github.com/org/infra/.worktrees/feat",
        "repo_root": "/home/ubuntu/code/github.com/org/infra",
        "bare_root": "/home/ubuntu/code/github.com/org/infra/.bare",
        "worktree_name": "feat",
        "is_default": false,
        "owner": "ubuntu"
      },
      "events": [
        {"event": "worktree_creating", "timestamp": "2026-06-01T11:00:00Z", "detail": "feat"},
        {"event": "worktree_created", "timestamp": "2026-06-01T11:00:03Z", "detail": "feat"}
      ],
      "status": "ready",
      "head_commit": "def456",
      "provisioned_at": "2026-06-01T11:00:00Z"
    }
  }
}`

// v1StateWithIDE is a v1 state file with an IDE instance.
const v1StateWithIDE = `{
  "version": "v1",
  "updated_at": "2026-06-01T10:00:00Z",
  "workspaces": {
    "backend": {
      "spec": {
        "name": "backend",
        "canonical_name": "backend",
        "vcs": {
          "host": "github.com",
          "auth_user": "bot",
          "repo": "org/backend",
          "branch": "main",
          "clone_url": "https://github.com/org/backend.git"
        },
        "pat_name": "gh-token",
        "project_root": "/home/ubuntu/code/github.com/org/backend/default",
        "repo_root": "/home/ubuntu/code/github.com/org/backend",
        "bare_root": "/home/ubuntu/code/github.com/org/backend/.bare",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu",
        "ide": {"adapter": "openvscode-server"}
      },
      "events": [
        {"event": "clone_started", "timestamp": "2026-06-01T10:00:00Z", "detail": ""},
        {"event": "worktree_created", "timestamp": "2026-06-01T10:00:06Z", "detail": "default"}
      ],
      "status": "ready",
      "ide": {
        "name": "backend",
        "adapter": "openvscode-server",
        "port": 9100,
        "events": [
          {"scope": "ide:backend", "event": "ide_started", "timestamp": "2026-06-01T10:00:07Z", "detail": "port=9100"},
          {"scope": "ide:backend", "event": "ide_ready", "timestamp": "2026-06-01T10:00:09Z", "detail": "port=9100"}
        ],
        "status": "ready"
      },
      "provisioned_at": "2026-06-01T10:00:00Z"
    }
  }
}`

// v1StateMultiRepo has two different repos in the same state file.
const v1StateMultiRepo = `{
  "version": "v1",
  "updated_at": "2026-06-01T12:00:00Z",
  "workspaces": {
    "infra": {
      "spec": {
        "name": "infra",
        "canonical_name": "infra",
        "vcs": {"host": "github.com", "repo": "org/infra", "branch": "main", "clone_url": "https://github.com/org/infra.git"},
        "pat_name": "gh-token",
        "project_root": "/home/ubuntu/code/github.com/org/infra/default",
        "repo_root": "/home/ubuntu/code/github.com/org/infra",
        "bare_root": "/home/ubuntu/code/github.com/org/infra/.bare",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu"
      },
      "events": [
        {"event": "clone_completed", "timestamp": "2026-06-01T10:00:05Z", "detail": ""}
      ],
      "status": "ready",
      "provisioned_at": "2026-06-01T10:00:00Z"
    },
    "backend": {
      "spec": {
        "name": "backend",
        "canonical_name": "backend",
        "vcs": {"host": "github.com", "repo": "org/backend", "branch": "main", "clone_url": "https://github.com/org/backend.git"},
        "pat_name": "gh-token",
        "project_root": "/home/ubuntu/code/github.com/org/backend/default",
        "repo_root": "/home/ubuntu/code/github.com/org/backend",
        "bare_root": "/home/ubuntu/code/github.com/org/backend/.bare",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu"
      },
      "events": [
        {"event": "clone_completed", "timestamp": "2026-06-01T11:00:05Z", "detail": ""}
      ],
      "status": "ready",
      "provisioned_at": "2026-06-01T11:00:00Z"
    }
  }
}`

// v2StateAlreadyMigrated is a v2 state file that should not be re-migrated.
const v2StateAlreadyMigrated = `{
  "version": "v2",
  "updated_at": "2026-06-01T10:00:00Z",
  "workspaces": {
    "infra": {
      "name": "infra",
      "repo": {"host": "github.com", "slug": "org/infra", "clone_url": "https://github.com/org/infra.git"},
      "repo_root": "/home/ubuntu/code/github.com/org/infra",
      "bare_root": "/home/ubuntu/code/github.com/org/infra/.bare",
      "owner": "ubuntu",
      "pat_name": "gh-token",
      "worktrees": [
        {"name": "default", "branch": "main", "project_root": "/home/ubuntu/code/github.com/org/infra/default", "is_default": true}
      ],
      "status": "ready"
    }
  }
}`

// v1StateWithCredentials has both workspaces and credentials.
const v1StateWithCredentials = `{
  "version": "v1",
  "updated_at": "2026-06-01T10:00:00Z",
  "workspaces": {
    "infra": {
      "spec": {
        "name": "infra",
        "canonical_name": "infra",
        "vcs": {"host": "github.com", "repo": "org/infra", "branch": "main"},
        "pat_name": "gh-token",
        "project_root": "/home/ubuntu/code/github.com/org/infra/default",
        "repo_root": "/home/ubuntu/code/github.com/org/infra",
        "bare_root": "/home/ubuntu/code/github.com/org/infra/.bare",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu"
      },
      "events": [
        {"event": "worktree_created", "timestamp": "2026-06-01T10:00:06Z", "detail": "default"}
      ],
      "status": "ready",
      "provisioned_at": "2026-06-01T10:00:00Z"
    }
  },
  "credentials": {
    "ubuntu": {
      "owner": "ubuntu",
      "git_hosts": ["github.com"],
      "sso_session": "dsc-session"
    }
  }
}`

func TestNeedsMigrationRaw_V2(t *testing.T) {
	if needsMigrationRaw([]byte(v2StateAlreadyMigrated)) {
		t.Error("v2 state should not need migration")
	}
}

func TestNeedsMigrationRaw_V1WithSlashKey(t *testing.T) {
	if !needsMigrationRaw([]byte(v1StateWithBranch)) {
		t.Error("v1 state with slash key should need migration")
	}
}

func TestNeedsMigrationRaw_V1WithWorktreeName(t *testing.T) {
	if !needsMigrationRaw([]byte(v1StateDefaultOnly)) {
		t.Error("v1 state with worktree_name in spec should need migration")
	}
}

func TestNeedsMigrationRaw_V1NoWorktreesWithSpec(t *testing.T) {
	// A v1 workspace with a spec object but no worktrees array.
	v1Minimal := `{
  "version": "v1",
  "workspaces": {
    "infra": {
      "spec": {"name": "infra", "vcs": {"host": "github.com", "repo": "org/infra"}},
      "status": "ready"
    }
  }
}`
	if !needsMigrationRaw([]byte(v1Minimal)) {
		t.Error("v1 state with spec but no worktrees should need migration")
	}
}

func TestNeedsMigrationRaw_NewFormatNoWorktrees(t *testing.T) {
	// New v2-style workspace (no spec, top-level name) written with version "v1"
	// but without a "spec" key — should NOT trigger migration.
	newStyleV1 := `{
  "version": "v1",
  "workspaces": {
    "ws1": {
      "name": "ws1",
      "repo": {"host": "github.com", "slug": "org/repo1"},
      "owner": "user",
      "status": "ready"
    }
  }
}`
	if needsMigrationRaw([]byte(newStyleV1)) {
		t.Error("new-format workspace without spec key should not trigger migration")
	}
}

func TestMigrateV1ToV2_DefaultOnly(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateDefaultOnly))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if sf.Version != StateVersionV2 {
		t.Errorf("version: expected %q, got %q", StateVersionV2, sf.Version)
	}

	if len(sf.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(sf.Workspaces))
	}

	ws := sf.Workspaces["infra"]
	if ws == nil {
		t.Fatal("expected workspace keyed as 'infra'")
	}

	if ws.Name != "infra" {
		t.Errorf("name: expected %q, got %q", "infra", ws.Name)
	}
	if ws.Repo.Host != "github.com" {
		t.Errorf("repo.host: expected %q, got %q", "github.com", ws.Repo.Host)
	}
	if ws.Repo.Slug != "org/infra" {
		t.Errorf("repo.slug: expected %q, got %q", "org/infra", ws.Repo.Slug)
	}
	if ws.Repo.CloneURL != "https://github.com/org/infra.git" {
		t.Errorf("repo.clone_url: expected %q, got %q", "https://github.com/org/infra.git", ws.Repo.CloneURL)
	}
	if ws.RepoRoot != "/home/ubuntu/code/github.com/org/infra" {
		t.Errorf("repo_root: expected %q, got %q", "/home/ubuntu/code/github.com/org/infra", ws.RepoRoot)
	}
	if ws.BareRoot != "/home/ubuntu/code/github.com/org/infra/.bare" {
		t.Errorf("bare_root: expected %q, got %q", "/home/ubuntu/code/github.com/org/infra/.bare", ws.BareRoot)
	}
	if ws.Owner != "ubuntu" {
		t.Errorf("owner: expected %q, got %q", "ubuntu", ws.Owner)
	}
	if ws.PatName != "gh-token" {
		t.Errorf("pat_name: expected %q, got %q", "gh-token", ws.PatName)
	}

	// Worktrees
	if len(ws.Worktrees) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(ws.Worktrees))
	}
	wt := ws.Worktrees[0]
	if wt.Name != "default" {
		t.Errorf("worktree.name: expected %q, got %q", "default", wt.Name)
	}
	if wt.Branch != "main" {
		t.Errorf("worktree.branch: expected %q, got %q", "main", wt.Branch)
	}
	if wt.ProjectRoot != "/home/ubuntu/code/github.com/org/infra/default" {
		t.Errorf("worktree.project_root: expected %q, got %q", "/home/ubuntu/code/github.com/org/infra/default", wt.ProjectRoot)
	}
	if !wt.IsDefault {
		t.Error("worktree.is_default: expected true")
	}
	if wt.HeadCommit != "abc123" {
		t.Errorf("worktree.head_commit: expected %q, got %q", "abc123", wt.HeadCommit)
	}

	// Events
	if len(ws.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(ws.Events))
	}

	// ProvisionedAt
	if ws.ProvisionedAt == nil {
		t.Fatal("expected provisioned_at to be set")
	}
	expectedTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if !ws.ProvisionedAt.Equal(expectedTime) {
		t.Errorf("provisioned_at: expected %v, got %v", expectedTime, *ws.ProvisionedAt)
	}

	// Status should be re-projected from events
	if ws.Status != domain.StatusReady {
		t.Errorf("status: expected %q, got %q", domain.StatusReady, ws.Status)
	}
}

func TestMigrateV1ToV2_WithBranch(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateWithBranch))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if len(sf.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace (grouped by repo), got %d", len(sf.Workspaces))
	}

	ws := sf.Workspaces["infra"]
	if ws == nil {
		t.Fatal("expected workspace keyed as 'infra'")
	}

	// Should have 2 worktrees: default and feat
	if len(ws.Worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(ws.Worktrees))
	}

	// Default should be first (sorted)
	if ws.Worktrees[0].Name != "default" {
		t.Errorf("first worktree: expected 'default', got %q", ws.Worktrees[0].Name)
	}
	if !ws.Worktrees[0].IsDefault {
		t.Error("first worktree should be default")
	}
	if ws.Worktrees[1].Name != "feat" {
		t.Errorf("second worktree: expected 'feat', got %q", ws.Worktrees[1].Name)
	}
	if ws.Worktrees[1].IsDefault {
		t.Error("second worktree should not be default")
	}
	if ws.Worktrees[1].Branch != "feat" {
		t.Errorf("second worktree branch: expected %q, got %q", "feat", ws.Worktrees[1].Branch)
	}
	if ws.Worktrees[1].ProjectRoot != "/home/ubuntu/code/github.com/org/infra/.worktrees/feat" {
		t.Errorf("second worktree project_root mismatch: %q", ws.Worktrees[1].ProjectRoot)
	}

	// Events should be merged and sorted by timestamp (5 total: 3 from default + 2 from feat)
	if len(ws.Events) != 5 {
		t.Fatalf("expected 5 merged events, got %d", len(ws.Events))
	}
	// Verify chronological order
	for i := 1; i < len(ws.Events); i++ {
		if ws.Events[i].Timestamp.Before(ws.Events[i-1].Timestamp) {
			t.Errorf("events not sorted: event[%d] (%v) before event[%d] (%v)",
				i, ws.Events[i].Timestamp, i-1, ws.Events[i-1].Timestamp)
		}
	}

	// ProvisionedAt should be the earliest (10:00, not 11:00)
	expectedTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	if !ws.ProvisionedAt.Equal(expectedTime) {
		t.Errorf("provisioned_at: expected %v, got %v", expectedTime, *ws.ProvisionedAt)
	}
}

func TestMigrateV1ToV2_WithIDE(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateWithIDE))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	ws := sf.Workspaces["backend"]
	if ws == nil {
		t.Fatal("expected workspace 'backend'")
	}

	// IDE should be migrated to map keyed by worktree name
	if ws.IDE == nil {
		t.Fatal("expected IDE map to be populated")
	}
	ide := ws.IDE["default"]
	if ide == nil {
		t.Fatal("expected IDE instance for 'default' worktree")
	}
	if ide.Adapter != "openvscode-server" {
		t.Errorf("ide.adapter: expected %q, got %q", "openvscode-server", ide.Adapter)
	}
	if ide.Port != 9100 {
		t.Errorf("ide.port: expected 9100, got %d", ide.Port)
	}
	if ide.Status != domain.StatusReady {
		t.Errorf("ide.status: expected %q, got %q", domain.StatusReady, ide.Status)
	}
	if len(ide.Events) != 2 {
		t.Fatalf("expected 2 IDE events, got %d", len(ide.Events))
	}
}

func TestMigrateV1ToV2_MultiRepo(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateMultiRepo))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if len(sf.Workspaces) != 2 {
		t.Fatalf("expected 2 workspaces (one per repo), got %d", len(sf.Workspaces))
	}

	if sf.Workspaces["infra"] == nil {
		t.Error("expected 'infra' workspace")
	}
	if sf.Workspaces["backend"] == nil {
		t.Error("expected 'backend' workspace")
	}

	if sf.Workspaces["infra"].Repo.Slug != "org/infra" {
		t.Errorf("infra repo.slug: expected %q, got %q", "org/infra", sf.Workspaces["infra"].Repo.Slug)
	}
	if sf.Workspaces["backend"].Repo.Slug != "org/backend" {
		t.Errorf("backend repo.slug: expected %q, got %q", "org/backend", sf.Workspaces["backend"].Repo.Slug)
	}
}

func TestMigrateV1ToV2_Idempotent(t *testing.T) {
	// Loading a v2 file should not re-migrate.
	if needsMigrationRaw([]byte(v2StateAlreadyMigrated)) {
		t.Error("v2 state should not need migration")
	}

	// Verify the data is intact after normal deserialization.
	var sf StateFile
	if err := json.Unmarshal([]byte(v2StateAlreadyMigrated), &sf); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	ws := sf.Workspaces["infra"]
	if ws == nil {
		t.Fatal("expected workspace 'infra'")
	}
	if len(ws.Worktrees) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(ws.Worktrees))
	}
}

func TestMigrateV1ToV2_PreservesCredentials(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateWithCredentials))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	if sf.Credentials == nil {
		t.Fatal("expected credentials to be preserved")
	}
	cs := sf.Credentials["ubuntu"]
	if cs == nil {
		t.Fatal("expected 'ubuntu' credential state")
	}
	if cs.Owner != "ubuntu" {
		t.Errorf("credential owner: expected %q, got %q", "ubuntu", cs.Owner)
	}
	if len(cs.GitHosts) != 1 || cs.GitHosts[0] != "github.com" {
		t.Errorf("credential git_hosts: expected [github.com], got %v", cs.GitHosts)
	}
}

func TestMigrateV1ToV2_VersionBumped(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateDefaultOnly))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if sf.Version != StateVersionV2 {
		t.Errorf("version: expected %q, got %q", StateVersionV2, sf.Version)
	}
}

func TestMigrateV1ToV2_EventsSortedChronologically(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateWithBranch))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	ws := sf.Workspaces["infra"]
	for i := 1; i < len(ws.Events); i++ {
		if ws.Events[i].Timestamp.Before(ws.Events[i-1].Timestamp) {
			t.Errorf("events not chronological: [%d]=%v before [%d]=%v",
				i, ws.Events[i].Timestamp, i-1, ws.Events[i-1].Timestamp)
		}
	}
}

// TestLoadState_MigratesV1OnLoad verifies the end-to-end flow: write a v1 file,
// load it through FileStore, and verify the migration happened transparently.
func TestLoadState_MigratesV1OnLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write a v1 state file directly.
	if err := os.WriteFile(path, []byte(v1StateWithBranch), 0664); err != nil {
		t.Fatal(err)
	}

	s := NewFileStore(path)
	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	// Should have 1 workspace (grouped from 2 old entries).
	if len(state.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace after migration, got %d", len(state.Workspaces))
	}

	ws := state.Workspaces["infra"]
	if ws == nil {
		t.Fatal("expected 'infra' workspace")
	}
	if len(ws.Worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(ws.Worktrees))
	}
}

// TestSave_WritesV2Format verifies that Save() writes the v2 version.
func TestSave_WritesV2Format(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s := NewFileStore(path)

	ws := map[string]*domain.Workspace{
		"infra": {
			Name:  "infra",
			Owner: "ubuntu",
			Worktrees: []domain.Worktree{
				{Name: "default", Branch: "main", IsDefault: true},
			},
		},
	}

	if err := s.Save(ws); err != nil {
		t.Fatal(err)
	}

	// Read raw to verify version.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatal(err)
	}
	if sf.Version != StateVersionV2 {
		t.Errorf("saved version: expected %q, got %q", StateVersionV2, sf.Version)
	}
}

// TestLoadState_MigratesAndNextSavePersistsV2 verifies the full lifecycle:
// load v1 -> transparent migration -> save -> reload as v2 without re-migration.
func TestLoadState_MigratesAndNextSavePersistsV2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Write v1 state.
	if err := os.WriteFile(path, []byte(v1StateDefaultOnly), 0664); err != nil {
		t.Fatal(err)
	}

	s := NewFileStore(path)

	// Load triggers migration.
	state, err := s.LoadState()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	// Save persists v2.
	if err := s.SaveState(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify file is now v2.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		t.Fatal(err)
	}
	if sf.Version != StateVersionV2 {
		t.Errorf("persisted version: expected %q, got %q", StateVersionV2, sf.Version)
	}

	// Reload — should not re-migrate.
	state2, err := s.LoadState()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	ws := state2.Workspaces["infra"]
	if ws == nil {
		t.Fatal("expected 'infra' workspace on second load")
	}
	if len(ws.Worktrees) != 1 {
		t.Fatalf("expected 1 worktree on second load, got %d", len(ws.Worktrees))
	}
}

// TestMigrateV1ToV2_EmptyRepoRootSkipped verifies entries with empty RepoRoot
// are skipped with a warning rather than causing a crash.
func TestMigrateV1ToV2_EmptyRepoRootSkipped(t *testing.T) {
	v1State := `{
  "version": "v1",
  "updated_at": "2026-06-01T10:00:00Z",
  "workspaces": {
    "corrupt": {
      "spec": {
        "name": "corrupt",
        "vcs": {"host": "github.com", "repo": "org/corrupt"},
        "project_root": "/some/path",
        "repo_root": "",
        "bare_root": "",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu"
      },
      "status": "ready"
    },
    "valid": {
      "spec": {
        "name": "valid",
        "vcs": {"host": "github.com", "repo": "org/valid", "branch": "main"},
        "project_root": "/home/ubuntu/code/github.com/org/valid/default",
        "repo_root": "/home/ubuntu/code/github.com/org/valid",
        "bare_root": "/home/ubuntu/code/github.com/org/valid/.bare",
        "worktree_name": "default",
        "is_default": true,
        "owner": "ubuntu"
      },
      "events": [
        {"event": "worktree_created", "timestamp": "2026-06-01T10:00:06Z", "detail": "default"}
      ],
      "status": "ready"
    }
  }
}`

	sf, err := migrateV1ToV2([]byte(v1State))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Only the valid entry should survive.
	if len(sf.Workspaces) != 1 {
		t.Fatalf("expected 1 workspace (corrupt skipped), got %d", len(sf.Workspaces))
	}
	if sf.Workspaces["valid"] == nil {
		t.Error("expected 'valid' workspace to survive")
	}
}

// TestMigrateV1ToV2_OldEventFormat verifies that old WorkspaceEventRecord
// format (without scope) is properly converted during migration.
func TestMigrateV1ToV2_OldEventFormat(t *testing.T) {
	sf, err := migrateV1ToV2([]byte(v1StateDefaultOnly))
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	ws := sf.Workspaces["infra"]
	for i, ev := range ws.Events {
		if ev.Scope.Kind != domain.ScopeKindWorkspace {
			t.Errorf("event[%d] scope.kind: expected %q, got %q", i, domain.ScopeKindWorkspace, ev.Scope.Kind)
		}
		if ev.Scope.Name != "infra" {
			t.Errorf("event[%d] scope.name: expected %q, got %q", i, "infra", ev.Scope.Name)
		}
	}
}

package domain

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Unit tests (no git needed) ---

func TestPrune_NotFound(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{}

	_, err := p.Prune(store, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing workspace")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrNotFound {
		t.Fatalf("expected NOT_FOUND, got %s", pe.Code)
	}
}

func TestPrune_NoNonDefaultWorktrees(t *testing.T) {
	store := newMemStore()
	store.instances["myrepo"] = &Workspace{
		Spec: WorkspaceSpec{
			Name:         "myrepo",
			IsDefault:    true,
			WorktreeName: "default",
			ProjectRoot:  "/tmp/fake/default",
			RepoRoot:     "/tmp/fake",
			BareRoot:     "/tmp/fake/.bare",
			Owner:        "user",
		},
		Status: StatusReady,
	}

	p := &Provisioner{}
	result, err := p.Prune(store, "myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Pruned) != 0 {
		t.Fatalf("expected empty pruned list, got %v", result.Pruned)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected empty skipped list, got %v", result.Skipped)
	}
	if result.Message != "No non-default worktrees to prune." {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

// --- Integration tests (require git) ---

func TestPrune_AllClean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "spike-a", "a.tf", "# a\n")
	addUpstreamBranch(t, dir, "spike-b", "b.tf", "# b\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	spikeARoot := filepath.Join(repoRoot, ".worktrees", "spike-a")
	spikeBRoot := filepath.Join(repoRoot, ".worktrees", "spike-b")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + two branch worktrees
	defaultSpec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "main"},
		ProjectRoot:  defaultRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}
	_, err := p.Provision(store, defaultSpec)
	if err != nil {
		t.Fatalf("default provision failed: %v", err)
	}

	spikeASpec := WorkspaceSpec{
		Name:         "myrepo/spike-a",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "spike-a"},
		ProjectRoot:  spikeARoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-a",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, err = p.Provision(store, spikeASpec)
	if err != nil {
		t.Fatalf("spike-a provision failed: %v", err)
	}

	spikeBSpec := WorkspaceSpec{
		Name:         "myrepo/spike-b",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "spike-b"},
		ProjectRoot:  spikeBRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-b",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, err = p.Provision(store, spikeBSpec)
	if err != nil {
		t.Fatalf("spike-b provision failed: %v", err)
	}

	// AC: Prune removes all clean non-default worktrees
	result, err := p.Prune(store, "myrepo")
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(result.Pruned) != 2 {
		t.Fatalf("expected 2 pruned, got %d: %v", len(result.Pruned), result.Pruned)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}

	// AC: State entries removed for pruned worktrees
	if store.instances["myrepo/spike-a"] != nil {
		t.Fatal("spike-a should be removed from state")
	}
	if store.instances["myrepo/spike-b"] != nil {
		t.Fatal("spike-b should be removed from state")
	}

	// AC: Default worktree is never pruned
	if store.instances["myrepo"] == nil {
		t.Fatal("default workspace should still be in state")
	}

	// Worktree directories should be gone
	if worktreeExists(spikeARoot) {
		t.Fatal("spike-a worktree directory should be removed")
	}
	if worktreeExists(spikeBRoot) {
		t.Fatal("spike-b worktree directory should be removed")
	}

	// Default directory should still exist
	if !worktreeExists(defaultRoot) {
		t.Fatal("default worktree should still exist")
	}

	// Message should say 2 worktrees pruned
	if result.Message != "2 worktrees pruned." {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

func TestPrune_MixedCleanDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "spike-a", "a.tf", "# a\n")
	addUpstreamBranch(t, dir, "spike-b", "b.tf", "# b\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	spikeARoot := filepath.Join(repoRoot, ".worktrees", "spike-a")
	spikeBRoot := filepath.Join(repoRoot, ".worktrees", "spike-b")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + two branch worktrees
	defaultSpec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "main"},
		ProjectRoot:  defaultRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, defaultSpec)

	spikeASpec := WorkspaceSpec{
		Name:         "myrepo/spike-a",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "spike-a"},
		ProjectRoot:  spikeARoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-a",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, spikeASpec)

	spikeBSpec := WorkspaceSpec{
		Name:         "myrepo/spike-b",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "spike-b"},
		ProjectRoot:  spikeBRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-b",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, spikeBSpec)

	// Make spike-a dirty
	os.WriteFile(filepath.Join(spikeARoot, "dirty.txt"), []byte("uncommitted\n"), 0644)

	// AC: Dirty worktrees are skipped with reason in the response
	result, err := p.Prune(store, "myrepo")
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(result.Pruned) != 1 {
		t.Fatalf("expected 1 pruned, got %d: %v", len(result.Pruned), result.Pruned)
	}
	if result.Pruned[0] != "myrepo/spike-b" {
		t.Fatalf("expected pruned=[myrepo/spike-b], got %v", result.Pruned)
	}

	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}
	if result.Skipped[0].Name != "myrepo/spike-a" {
		t.Fatalf("expected skipped name=myrepo/spike-a, got %s", result.Skipped[0].Name)
	}
	if result.Skipped[0].Reason != "uncommitted changes" {
		t.Fatalf("expected reason='uncommitted changes', got %s", result.Skipped[0].Reason)
	}

	// AC: State entries removed only for pruned worktrees
	if store.instances["myrepo/spike-b"] != nil {
		t.Fatal("spike-b should be removed from state")
	}
	if store.instances["myrepo/spike-a"] == nil {
		t.Fatal("spike-a should still be in state (dirty)")
	}
	if store.instances["myrepo"] == nil {
		t.Fatal("default workspace should still be in state")
	}

	// Message
	if result.Message != "1 worktree pruned, 1 skipped." {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

func TestPrune_AllDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "spike-a", "a.tf", "# a\n")
	addUpstreamBranch(t, dir, "spike-b", "b.tf", "# b\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	spikeARoot := filepath.Join(repoRoot, ".worktrees", "spike-a")
	spikeBRoot := filepath.Join(repoRoot, ".worktrees", "spike-b")

	store := newMemStore()
	p := &Provisioner{}

	defaultSpec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "main"},
		ProjectRoot:  defaultRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, defaultSpec)

	spikeASpec := WorkspaceSpec{
		Name:         "myrepo/spike-a",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "spike-a"},
		ProjectRoot:  spikeARoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-a",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, spikeASpec)

	spikeBSpec := WorkspaceSpec{
		Name:         "myrepo/spike-b",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "spike-b"},
		ProjectRoot:  spikeBRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-b",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, spikeBSpec)

	// Make both dirty
	os.WriteFile(filepath.Join(spikeARoot, "dirty.txt"), []byte("uncommitted\n"), 0644)
	os.WriteFile(filepath.Join(spikeBRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)

	result, err := p.Prune(store, "myrepo")
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(result.Pruned) != 0 {
		t.Fatalf("expected 0 pruned, got %d: %v", len(result.Pruned), result.Pruned)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}

	// All state entries should still exist
	if store.instances["myrepo"] == nil {
		t.Fatal("default should still be in state")
	}
	if store.instances["myrepo/spike-a"] == nil {
		t.Fatal("spike-a should still be in state")
	}
	if store.instances["myrepo/spike-b"] == nil {
		t.Fatal("spike-b should still be in state")
	}

	// Message should mention 0 pruned, 2 skipped
	if result.Message != "0 worktrees pruned, 2 skipped." {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

func TestPrune_OnlyDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{}

	defaultSpec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "main"},
		ProjectRoot:  defaultRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}
	_, err := p.Provision(store, defaultSpec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// AC: No-op case (no non-default worktrees) returns success with empty lists
	result, err := p.Prune(store, "myrepo")
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(result.Pruned) != 0 {
		t.Fatalf("expected 0 pruned, got %d", len(result.Pruned))
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(result.Skipped))
	}
	if result.Message != "No non-default worktrees to prune." {
		t.Fatalf("unexpected message: %s", result.Message)
	}

	// Default should still exist
	if store.instances["myrepo"] == nil {
		t.Fatal("default workspace should still be in state")
	}
}

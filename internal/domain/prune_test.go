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
		Name:   "myrepo",
		Owner:  "user",
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: "/tmp/fake/default", IsDefault: true},
		},
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

	store := newMemStore()
	p := &Provisioner{}

	// Provision default
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, err := p.Provision(store, defaultParams)
	if err != nil {
		t.Fatalf("default provision failed: %v", err)
	}

	// Add branch worktrees via AddWorktree
	spikeAResult, err := p.AddWorktree(store, "myrepo", "spike-a")
	if err != nil {
		t.Fatalf("AddWorktree spike-a failed: %v", err)
	}
	spikeBResult, err := p.AddWorktree(store, "myrepo", "spike-b")
	if err != nil {
		t.Fatalf("AddWorktree spike-b failed: %v", err)
	}

	spikeARoot := spikeAResult.ProjectRoot
	spikeBRoot := spikeBResult.ProjectRoot

	// AC: Prune removes all clean non-default worktrees from the aggregate
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

	// AC: Non-default worktrees removed from aggregate's Worktrees slice
	ws := store.instances["myrepo"]
	if ws == nil {
		t.Fatal("default workspace should still be in state")
	}
	for _, wt := range ws.Worktrees {
		if !wt.IsDefault {
			t.Fatalf("non-default worktree '%s' should have been pruned from aggregate", wt.Name)
		}
	}

	// AC: Default worktree is never pruned
	if ws.DefaultWorktree() == nil {
		t.Fatal("default worktree should still exist in aggregate")
	}

	// Worktree directories should be gone
	if spikeARoot != "" && worktreeExists(spikeARoot) {
		t.Fatal("spike-a worktree directory should be removed")
	}
	if spikeBRoot != "" && worktreeExists(spikeBRoot) {
		t.Fatal("spike-b worktree directory should be removed")
	}

	// Default directory should still exist
	defaultRoot := ws.DefaultProjectRoot()
	if defaultRoot != "" && !worktreeExists(defaultRoot) {
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

	store := newMemStore()
	p := &Provisioner{}

	// Provision default
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, _ = p.Provision(store, defaultParams)

	// Add branch worktrees via AddWorktree
	spikeAResult, _ := p.AddWorktree(store, "myrepo", "spike-a")
	_, _ = p.AddWorktree(store, "myrepo", "spike-b")

	spikeARoot := spikeAResult.ProjectRoot

	// Make spike-a dirty
	if spikeARoot != "" {
		os.WriteFile(filepath.Join(spikeARoot, "dirty.txt"), []byte("uncommitted\n"), 0644)
	}

	// AC: Dirty worktrees are skipped with reason in the response
	result, err := p.Prune(store, "myrepo")
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	if len(result.Pruned) != 1 {
		t.Fatalf("expected 1 pruned, got %d: %v", len(result.Pruned), result.Pruned)
	}
	if result.Pruned[0] != "spike-b" {
		t.Fatalf("expected pruned=[spike-b], got %v", result.Pruned)
	}

	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d: %v", len(result.Skipped), result.Skipped)
	}
	if result.Skipped[0].Name != "spike-a" {
		t.Fatalf("expected skipped name=spike-a, got %s", result.Skipped[0].Name)
	}
	if result.Skipped[0].Reason != "uncommitted changes" {
		t.Fatalf("expected reason='uncommitted changes', got %s", result.Skipped[0].Reason)
	}

	// AC: Pruned worktree removed from aggregate, dirty one retained
	ws := store.instances["myrepo"]
	if ws.FindWorktree("spike-b") != nil {
		t.Fatal("spike-b should be removed from aggregate worktrees")
	}
	if ws.FindWorktree("spike-a") == nil {
		t.Fatal("spike-a should still be in aggregate (dirty)")
	}
	if ws.DefaultWorktree() == nil {
		t.Fatal("default should still be in aggregate")
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

	store := newMemStore()
	p := &Provisioner{}

	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, _ = p.Provision(store, defaultParams)

	// Add branch worktrees via AddWorktree
	spikeAResult, _ := p.AddWorktree(store, "myrepo", "spike-a")
	spikeBResult, _ := p.AddWorktree(store, "myrepo", "spike-b")

	spikeARoot := spikeAResult.ProjectRoot
	spikeBRoot := spikeBResult.ProjectRoot

	// Make both dirty
	if spikeARoot != "" {
		os.WriteFile(filepath.Join(spikeARoot, "dirty.txt"), []byte("uncommitted\n"), 0644)
	}
	if spikeBRoot != "" {
		os.WriteFile(filepath.Join(spikeBRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)
	}

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

	// All worktrees should still be in aggregate
	ws := store.instances["myrepo"]
	if ws == nil {
		t.Fatal("workspace should still be in state")
	}
	if ws.FindWorktree("spike-a") == nil {
		t.Fatal("spike-a should still be in aggregate")
	}
	if ws.FindWorktree("spike-b") == nil {
		t.Fatal("spike-b should still be in aggregate")
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

	store := newMemStore()
	p := &Provisioner{}

	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, err := p.Provision(store, defaultParams)
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

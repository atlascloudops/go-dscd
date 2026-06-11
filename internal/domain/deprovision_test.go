package domain

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Unit tests (no git needed) ---

func TestDeprovision_NotFound(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{}

	_, err := p.Deprovision(store, "nonexistent", false)
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

func TestDeprovisionAll_NotFound(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{}

	_, err := p.DeprovisionAll(store, "nonexistent", false)
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

// --- Integration tests (require git) ---

func TestDeprovision_CleanWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: defaultRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, err := p.Provision(store, defaultParams)
	if err != nil {
		t.Fatalf("default provision failed: %v", err)
	}

	// Provision feature worktree — in the new model this creates a second workspace aggregate
	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: featureRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, err = p.Provision(store, featureParams)
	if err != nil {
		t.Fatalf("feature provision failed: %v", err)
	}

	// AC: Clean worktree delete succeeds
	result, err := p.Deprovision(store, "myrepo/feature-vpc", false)
	if err != nil {
		t.Fatalf("deprovision failed: %v", err)
	}

	if len(result.Removed) != 1 || result.Removed[0] != "myrepo/feature-vpc" {
		t.Fatalf("expected removed=[myrepo/feature-vpc], got %v", result.Removed)
	}

	// AC: State entry removed
	if store.instances["myrepo/feature-vpc"] != nil {
		t.Fatal("feature workspace should be removed from state")
	}

	// Default workspace should still exist
	if store.instances["myrepo"] == nil {
		t.Fatal("default workspace should still be in state")
	}
}

func TestDeprovision_DirtyWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + feature
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: defaultRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, err := p.Provision(store, defaultParams)
	if err != nil {
		t.Fatalf("default provision failed: %v", err)
	}

	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: featureRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, err = p.Provision(store, featureParams)
	if err != nil {
		t.Fatalf("feature provision failed: %v", err)
	}

	// Make the worktree dirty
	os.WriteFile(filepath.Join(featureRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)

	// AC: Dirty worktree delete returns WORKTREE_DIRTY
	_, err = p.Deprovision(store, "myrepo/feature-vpc", false)
	if err == nil {
		t.Fatal("expected error for dirty worktree")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrWorktreeDirty {
		t.Fatalf("expected WORKTREE_DIRTY, got %s", pe.Code)
	}

	// Workspace should still exist in state
	if store.instances["myrepo/feature-vpc"] == nil {
		t.Fatal("workspace should still be in state after dirty guard")
	}
}

func TestDeprovision_ForceDeleteDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + feature
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: defaultRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, _ = p.Provision(store, defaultParams)

	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: featureRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, _ = p.Provision(store, featureParams)

	// Make dirty
	os.WriteFile(filepath.Join(featureRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)

	// AC: --force bypasses dirty check
	result, err := p.Deprovision(store, "myrepo/feature-vpc", true)
	if err != nil {
		t.Fatalf("force deprovision should succeed: %v", err)
	}

	if len(result.Removed) != 1 || result.Removed[0] != "myrepo/feature-vpc" {
		t.Fatalf("expected removed=[myrepo/feature-vpc], got %v", result.Removed)
	}

	if store.instances["myrepo/feature-vpc"] != nil {
		t.Fatal("workspace should be removed from state after force delete")
	}
}

func TestDeprovisionAll_FullRemoval(t *testing.T) {
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

	// Provision default
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: defaultRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, _ = p.Provision(store, defaultParams)

	// AC: deprovision removes workspace, bare clone, and repo container
	result, err := p.DeprovisionAll(store, "myrepo", true)
	if err != nil {
		t.Fatalf("deprovision all failed: %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("expected 1 removed, got %d: %v", len(result.Removed), result.Removed)
	}

	// AC: State entry removed
	if store.instances["myrepo"] != nil {
		t.Fatal("workspace should be removed from state")
	}

	// Bare clone should be gone
	if dirExists(bareRoot) {
		t.Fatal("bare clone should be removed")
	}

	// Repo root should be gone
	if dirExists(repoRoot) {
		t.Fatal("repo root should be removed")
	}
}

func TestDeprovisionAll_DirtyGuard(t *testing.T) {
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

	// Provision default
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: defaultRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, _ = p.Provision(store, defaultParams)

	// Make the default worktree dirty
	os.WriteFile(filepath.Join(defaultRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)

	// AC: checks for uncommitted changes (unless --force)
	_, err := p.DeprovisionAll(store, "myrepo", false)
	if err == nil {
		t.Fatal("expected error for dirty worktree in deprovision mode")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrWorktreeDirty {
		t.Fatalf("expected WORKTREE_DIRTY, got %s", pe.Code)
	}

	// Everything should still exist
	if store.instances["myrepo"] == nil {
		t.Fatal("workspace should still be in state after dirty guard")
	}
}

func TestIsWorktreeDirty_Clean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "repo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "repo",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: defaultRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	dirty, err := IsWorktreeDirty(defaultRoot, currentUser())
	if err != nil {
		t.Fatalf("IsWorktreeDirty failed: %v", err)
	}
	if dirty {
		t.Fatal("expected clean worktree")
	}
}

func TestIsWorktreeDirty_Dirty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "repo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "repo",
			VCS:   VCSTarget{Host: "github.com", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		ProjectRoot: defaultRoot,
		RepoRoot:    repoRoot,
		BareRoot:    bareRoot,
	}
	_, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// Create uncommitted file
	os.WriteFile(filepath.Join(defaultRoot, "new-file.txt"), []byte("hello\n"), 0644)

	dirty, err := IsWorktreeDirty(defaultRoot, currentUser())
	if err != nil {
		t.Fatalf("IsWorktreeDirty failed: %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty worktree")
	}
}

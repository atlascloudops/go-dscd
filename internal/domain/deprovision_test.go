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

func TestDeprovision_CannotDeleteDefault(t *testing.T) {
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
	_, err := p.Deprovision(store, "myrepo", false)
	if err == nil {
		t.Fatal("expected error when deleting default worktree")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrCannotDeleteDefault {
		t.Fatalf("expected CANNOT_DELETE_DEFAULT, got %s", pe.Code)
	}
	if pe.Message == "" {
		t.Fatal("expected non-empty message with --all hint")
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

	// Provision feature worktree
	featureSpec := WorkspaceSpec{
		Name:         "myrepo/feature-vpc",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "feature-vpc"},
		ProjectRoot:  featureRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "feature-vpc",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, err = p.Provision(store, featureSpec)
	if err != nil {
		t.Fatalf("feature provision failed: %v", err)
	}

	// AC: Clean worktree delete succeeds via git worktree remove
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

	// Worktree directory should be gone
	if worktreeExists(featureRoot) {
		t.Fatal("feature worktree directory should be removed")
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

	featureSpec := WorkspaceSpec{
		Name:         "myrepo/feature-vpc",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "feature-vpc"},
		ProjectRoot:  featureRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "feature-vpc",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, err = p.Provision(store, featureSpec)
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

	featureSpec := WorkspaceSpec{
		Name:         "myrepo/feature-vpc",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "feature-vpc"},
		ProjectRoot:  featureRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "feature-vpc",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, featureSpec)

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
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + feature
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

	featureSpec := WorkspaceSpec{
		Name:         "myrepo/feature-vpc",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "feature-vpc"},
		ProjectRoot:  featureRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "feature-vpc",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, featureSpec)

	// AC: --all removes all worktrees, bare clone, and repo container
	result, err := p.DeprovisionAll(store, "myrepo", true)
	if err != nil {
		t.Fatalf("deprovision all failed: %v", err)
	}

	if len(result.Removed) != 2 {
		t.Fatalf("expected 2 removed, got %d: %v", len(result.Removed), result.Removed)
	}

	// AC: State entries removed for all deleted workspaces
	if store.instances["myrepo"] != nil {
		t.Fatal("default workspace should be removed from state")
	}
	if store.instances["myrepo/feature-vpc"] != nil {
		t.Fatal("feature workspace should be removed from state")
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
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + feature
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

	featureSpec := WorkspaceSpec{
		Name:         "myrepo/feature-vpc",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "feature-vpc"},
		ProjectRoot:  featureRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "feature-vpc",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, _ = p.Provision(store, featureSpec)

	// Make the default worktree dirty
	os.WriteFile(filepath.Join(defaultRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)

	// AC: --all checks all worktrees for uncommitted changes (unless --force)
	_, err := p.DeprovisionAll(store, "myrepo", false)
	if err == nil {
		t.Fatal("expected error for dirty worktree in --all mode")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrWorktreeDirty {
		t.Fatalf("expected WORKTREE_DIRTY, got %s", pe.Code)
	}

	// Everything should still exist
	if store.instances["myrepo"] == nil || store.instances["myrepo/feature-vpc"] == nil {
		t.Fatal("all workspaces should still be in state after dirty guard")
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

	spec := WorkspaceSpec{
		Name:         "repo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "main"},
		ProjectRoot:  defaultRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}
	_, err := p.Provision(store, spec)
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

	spec := WorkspaceSpec{
		Name:         "repo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "main"},
		ProjectRoot:  defaultRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}
	_, err := p.Provision(store, spec)
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

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

func TestDeprovisionWorktree_NotFound(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{}

	_, err := p.DeprovisionWorktree(store, "nonexistent", "feat", false)
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

func TestDeprovisionWorktree_BranchNotFound(t *testing.T) {
	store := newMemStore()
	store.instances["myrepo"] = &Workspace{
		Name:     "myrepo",
		Owner:    "user",
		RepoRoot: "/tmp/fake",
		BareRoot: "/tmp/fake/.bare",
		Worktrees: []Worktree{
			{Name: "default", Branch: "main", ProjectRoot: "/tmp/fake/default", IsDefault: true},
		},
	}

	p := &Provisioner{}
	_, err := p.DeprovisionWorktree(store, "myrepo", "nonexistent-branch", false)
	if err == nil {
		t.Fatal("expected error for missing worktree branch")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrNotFound {
		t.Fatalf("expected NOT_FOUND, got %s", pe.Code)
	}
}

func TestDeprovisionWorktree_CannotDeleteDefault(t *testing.T) {
	store := newMemStore()
	store.instances["myrepo"] = &Workspace{
		Name:     "myrepo",
		Owner:    "user",
		RepoRoot: "/tmp/fake",
		BareRoot: "/tmp/fake/.bare",
		Worktrees: []Worktree{
			{Name: "default", Branch: "main", ProjectRoot: "/tmp/fake/default", IsDefault: true},
		},
	}

	p := &Provisioner{}
	_, err := p.DeprovisionWorktree(store, "myrepo", "main", false)
	if err == nil {
		t.Fatal("expected error when removing default worktree")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrCannotDeleteDefault {
		t.Fatalf("expected CANNOT_DELETE_DEFAULT, got %s", pe.Code)
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

	// Provision feature worktree — in the new model this creates a second workspace aggregate
	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
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

	// AC: RemovedWorktrees includes removed worktree names
	if len(result.RemovedWorktrees) == 0 {
		t.Fatal("expected RemovedWorktrees to be non-empty")
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

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + feature
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

	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, err = p.Provision(store, featureParams)
	if err != nil {
		t.Fatalf("feature provision failed: %v", err)
	}

	// Make the worktree dirty — find the project root from the workspace
	featureWs := store.instances["myrepo/feature-vpc"]
	featureRoot := featureWs.DefaultProjectRoot()
	if featureRoot == "" && len(featureWs.Worktrees) > 0 {
		featureRoot = featureWs.Worktrees[0].ProjectRoot
	}
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

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + feature
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, _ = p.Provision(store, defaultParams)

	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, _ = p.Provision(store, featureParams)

	// Make dirty
	featureWs := store.instances["myrepo/feature-vpc"]
	featureRoot := ""
	if len(featureWs.Worktrees) > 0 {
		featureRoot = featureWs.Worktrees[0].ProjectRoot
	}
	if featureRoot != "" {
		os.WriteFile(filepath.Join(featureRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)
	}

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

func TestDeprovision_FullRemoval(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

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

	ws := store.instances["myrepo"]
	repoRoot := ws.RepoRoot
	bareRoot := ws.BareRoot

	// AC: deprovision removes workspace, bare clone, and repo container
	result, err := p.Deprovision(store, "myrepo", true)
	if err != nil {
		t.Fatalf("deprovision failed: %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("expected 1 removed, got %d: %v", len(result.Removed), result.Removed)
	}

	// AC: RemovedWorktrees includes worktree names
	if len(result.RemovedWorktrees) == 0 {
		t.Fatal("expected RemovedWorktrees to be non-empty")
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

func TestDeprovision_DirtyGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

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

	// Make the default worktree dirty
	ws := store.instances["myrepo"]
	defaultRoot := ws.DefaultProjectRoot()
	os.WriteFile(filepath.Join(defaultRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)

	// AC: checks for uncommitted changes (unless --force)
	_, err := p.Deprovision(store, "myrepo", false)
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

func TestDeprovisionWorktree_RemoveNonDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

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

	// Provision feature worktree as a child workspace
	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	featureWs, err := p.Provision(store, featureParams)
	if err != nil {
		t.Fatalf("feature provision failed: %v", err)
	}

	// Get the feature worktree's project root
	featureRoot := ""
	if len(featureWs.Worktrees) > 0 {
		featureRoot = featureWs.Worktrees[0].ProjectRoot
	}

	// Add the feature worktree to the myrepo aggregate for the new model
	ws := store.instances["myrepo"]
	ws.Worktrees = append(ws.Worktrees, Worktree{
		Name:        "feature-vpc",
		Branch:      "feature-vpc",
		ProjectRoot: featureRoot,
		IsDefault:   false,
	})

	// AC: DeprovisionWorktree removes single worktree by branch
	result, err := p.DeprovisionWorktree(store, "myrepo", "feature-vpc", false)
	if err != nil {
		t.Fatalf("deprovision worktree failed: %v", err)
	}

	if len(result.RemovedWorktrees) != 1 || result.RemovedWorktrees[0] != "feature-vpc" {
		t.Fatalf("expected removed_worktrees=[feature-vpc], got %v", result.RemovedWorktrees)
	}

	// AC: Worktree entry removed from aggregate
	ws = store.instances["myrepo"]
	if ws == nil {
		t.Fatal("workspace should still exist in state")
	}
	if ws.FindWorktreeByBranch("feature-vpc") != nil {
		t.Fatal("feature-vpc worktree should be removed from aggregate")
	}

	// AC: Default worktree still present
	if ws.DefaultWorktree() == nil {
		t.Fatal("default worktree should still be present")
	}
}

func TestDeprovisionWorktree_DirtyGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default + feature
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	_, _ = p.Provision(store, defaultParams)

	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	featureWs, _ := p.Provision(store, featureParams)

	// Get feature worktree project root
	featureRoot := ""
	if len(featureWs.Worktrees) > 0 {
		featureRoot = featureWs.Worktrees[0].ProjectRoot
	}

	// Add to aggregate
	ws := store.instances["myrepo"]
	ws.Worktrees = append(ws.Worktrees, Worktree{
		Name:        "feature-vpc",
		Branch:      "feature-vpc",
		ProjectRoot: featureRoot,
		IsDefault:   false,
	})

	// Make the worktree dirty
	if featureRoot != "" {
		os.WriteFile(filepath.Join(featureRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)
	}

	// AC: Dirty worktree returns WORKTREE_DIRTY
	_, err := p.DeprovisionWorktree(store, "myrepo", "feature-vpc", false)
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

	// Worktree should still be in aggregate
	if ws.FindWorktreeByBranch("feature-vpc") == nil {
		t.Fatal("feature-vpc should still be in aggregate after dirty guard")
	}
}

func TestDeprovisionWorktree_ForceDeleteDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

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

	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	featureWs, _ := p.Provision(store, featureParams)

	featureRoot := ""
	if len(featureWs.Worktrees) > 0 {
		featureRoot = featureWs.Worktrees[0].ProjectRoot
	}

	// Add to aggregate
	ws := store.instances["myrepo"]
	ws.Worktrees = append(ws.Worktrees, Worktree{
		Name:        "feature-vpc",
		Branch:      "feature-vpc",
		ProjectRoot: featureRoot,
		IsDefault:   false,
	})

	// Make dirty
	if featureRoot != "" {
		os.WriteFile(filepath.Join(featureRoot, "dirty.txt"), []byte("uncommitted\n"), 0644)
	}

	// AC: --force bypasses dirty check
	result, err := p.DeprovisionWorktree(store, "myrepo", "feature-vpc", true)
	if err != nil {
		t.Fatalf("force deprovision worktree should succeed: %v", err)
	}

	if len(result.RemovedWorktrees) != 1 || result.RemovedWorktrees[0] != "feature-vpc" {
		t.Fatalf("expected removed_worktrees=[feature-vpc], got %v", result.RemovedWorktrees)
	}
}

func TestIsWorktreeDirty_Clean(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "repo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	ws, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	defaultRoot := ws.DefaultProjectRoot()
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "repo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: dir,
	}
	ws, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	defaultRoot := ws.DefaultProjectRoot()

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

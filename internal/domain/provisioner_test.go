package domain

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type memStore struct {
	instances map[string]*WorkspaceInstance
	locked    bool
}

func newMemStore() *memStore {
	return &memStore{instances: make(map[string]*WorkspaceInstance)}
}

func (m *memStore) Load() (map[string]*WorkspaceInstance, error) {
	return m.instances, nil
}

func (m *memStore) Save(instances map[string]*WorkspaceInstance) error {
	m.instances = instances
	return nil
}

func (m *memStore) WithLock(fn func() error) error {
	m.locked = true
	defer func() { m.locked = false }()
	return fn()
}

// runGit is a test helper that runs git commands and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// createUpstreamRepo creates a local bare repo with an initial commit on main,
// suitable as a clone source for tests. Returns the path to the bare upstream.
func createUpstreamRepo(t *testing.T, dir string) string {
	t.Helper()
	upstreamBare := filepath.Join(dir, "upstream.git")
	runGit(t, "", "init", "--bare", upstreamBare)

	scratchClone := filepath.Join(dir, "scratch")
	runGit(t, "", "clone", upstreamBare, scratchClone)
	os.WriteFile(filepath.Join(scratchClone, "README.md"), []byte("# test\n"), 0644)
	runGit(t, scratchClone, "add", ".")
	runGit(t, scratchClone, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "init")
	runGit(t, scratchClone, "push", "origin", "main")
	return upstreamBare
}

// addUpstreamBranch creates a branch on the upstream via the scratch clone.
func addUpstreamBranch(t *testing.T, dir, branch, filename, content string) {
	t.Helper()
	scratchClone := filepath.Join(dir, "scratch")
	runGit(t, scratchClone, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(scratchClone, filename), []byte(content), 0644)
	runGit(t, scratchClone, "add", ".")
	runGit(t, scratchClone, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "add "+filename)
	runGit(t, scratchClone, "push", "origin", branch)
}

// --- Unit tests (no git needed) ---

func TestValidateSpec_MissingFields(t *testing.T) {
	err := validateSpec(WorkspaceSpec{})
	if err == nil {
		t.Fatal("expected error for empty spec")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrSpecInvalid {
		t.Fatalf("expected SPEC_INVALID, got %s", pe.Code)
	}
	for _, field := range []string{"name", "vcs.clone_url", "vcs.branch", "project_root", "repo_root", "bare_root", "worktree_name", "owner"} {
		if !strings.Contains(pe.Detail, field) {
			t.Fatalf("expected detail to contain %q, got %q", field, pe.Detail)
		}
	}
}

func TestValidateSpec_Valid(t *testing.T) {
	dir := t.TempDir()
	spec := WorkspaceSpec{
		Name:         "test",
		VCS:          VCSTarget{CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot:  filepath.Join(dir, "default"),
		RepoRoot:     dir,
		BareRoot:     filepath.Join(dir, ".bare"),
		WorktreeName: "default",
		Owner:        "user",
	}
	if err := validateSpec(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSpec_MissingRepoRoot(t *testing.T) {
	spec := WorkspaceSpec{
		Name:         "test",
		VCS:          VCSTarget{CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot:  "/tmp/test",
		BareRoot:     "/tmp/.bare",
		WorktreeName: "default",
		Owner:        "user",
	}
	err := validateSpec(spec)
	if err == nil {
		t.Fatal("expected error when repo_root is empty")
	}
	pe := err.(*ProvisionError)
	if !strings.Contains(pe.Detail, "repo_root") {
		t.Fatalf("expected detail to mention repo_root, got %q", pe.Detail)
	}
}

func TestValidateSpec_MissingBareRoot(t *testing.T) {
	spec := WorkspaceSpec{
		Name:         "test",
		VCS:          VCSTarget{CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot:  "/tmp/test",
		RepoRoot:     "/tmp",
		WorktreeName: "default",
		Owner:        "user",
	}
	err := validateSpec(spec)
	if err == nil {
		t.Fatal("expected error when bare_root is empty")
	}
	pe := err.(*ProvisionError)
	if !strings.Contains(pe.Detail, "bare_root") {
		t.Fatalf("expected detail to mention bare_root, got %q", pe.Detail)
	}
}

func TestValidateSpec_MissingWorktreeName(t *testing.T) {
	spec := WorkspaceSpec{
		Name:        "test",
		VCS:         VCSTarget{CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot: "/tmp/test",
		RepoRoot:    "/tmp",
		BareRoot:    "/tmp/.bare",
		Owner:       "user",
	}
	err := validateSpec(spec)
	if err == nil {
		t.Fatal("expected error when worktree_name is empty")
	}
	pe := err.(*ProvisionError)
	if !strings.Contains(pe.Detail, "worktree_name") {
		t.Fatalf("expected detail to mention worktree_name, got %q", pe.Detail)
	}
}

func TestProvision_IdempotentWithGitDir(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "repo")
	projectRoot := filepath.Join(repoRoot, "default")
	bareRoot := filepath.Join(repoRoot, ".bare")

	// Simulate existing worktree with .git directory
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "test",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        "testuser",
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.State != StateReady {
		t.Fatalf("expected ready, got %s", inst.State)
	}
	if !inst.CloneExists {
		t.Fatal("expected clone_exists=true")
	}

	saved := store.instances["test"]
	if saved == nil {
		t.Fatal("workspace not persisted")
	}
	if saved.State != StateReady {
		t.Fatalf("persisted state should be ready, got %s", saved.State)
	}
}

func TestProvision_IdempotentWithGitFile(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "repo")
	projectRoot := filepath.Join(repoRoot, ".worktrees", "feature")
	bareRoot := filepath.Join(repoRoot, ".bare")

	// Simulate existing worktree with .git as a file (worktree pointer)
	os.MkdirAll(projectRoot, 0755)
	os.WriteFile(filepath.Join(projectRoot, ".git"), []byte("gitdir: ../../.bare/worktrees/feature\n"), 0644)

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "test/feature",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "https://github.com/org/repo.git", Branch: "feature"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "feature",
		IsDefault:    false,
		Owner:        "testuser",
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.State != StateReady {
		t.Fatalf("expected ready, got %s", inst.State)
	}
	if !inst.CloneExists {
		t.Fatal("expected clone_exists=true for worktree file")
	}
}

func TestProvision_InvalidSpec(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{}

	_, err := p.Provision(store, WorkspaceSpec{})
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrSpecInvalid {
		t.Fatalf("expected SPEC_INVALID, got %s", pe.Code)
	}
}

func TestWorktreeExists_NoPath(t *testing.T) {
	if worktreeExists("/nonexistent/path") {
		t.Fatal("expected false for nonexistent path")
	}
}

func TestWorktreeExists_GitDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	if !worktreeExists(dir) {
		t.Fatal("expected true for .git directory")
	}
}

func TestWorktreeExists_GitFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ../some/path\n"), 0644)
	if !worktreeExists(dir) {
		t.Fatal("expected true for .git file")
	}
}

func TestDirExists(t *testing.T) {
	dir := t.TempDir()
	if !dirExists(dir) {
		t.Fatal("expected true for existing dir")
	}
	if dirExists(filepath.Join(dir, "nope")) {
		t.Fatal("expected false for nonexistent dir")
	}
	f := filepath.Join(dir, "file.txt")
	os.WriteFile(f, []byte("hi"), 0644)
	if dirExists(f) {
		t.Fatal("expected false for regular file")
	}
}

// --- Integration tests (require git) ---

func TestProvisionBareCloneAndDefault_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "myrepo",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// AC: First provision creates .bare/ and default/
	if !dirExists(bareRoot) {
		t.Fatal(".bare/ directory was not created")
	}
	if !worktreeExists(projectRoot) {
		t.Fatal("default/ worktree was not created")
	}
	if inst.State != StateReady {
		t.Fatalf("expected ready, got %s", inst.State)
	}
	if inst.Status != "SYNCED" {
		t.Fatalf("expected SYNCED, got %s", inst.Status)
	}
	if inst.HeadCommit == "" {
		t.Fatal("expected non-empty head commit")
	}

	// Verify .git in default/ is a file (worktree), not a directory
	gitPath := filepath.Join(projectRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("stat .git: %v", err)
	}
	if info.IsDir() {
		t.Fatal(".git in worktree should be a file, not a directory")
	}

	// AC: Idempotent re-provision returns ready without re-cloning
	inst2, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("idempotent provision failed: %v", err)
	}
	if inst2.State != StateReady {
		t.Fatalf("expected ready on re-provision, got %s", inst2.State)
	}
}

func TestProvisionWorktree_FromExistingBare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	// Step 1: Provision default (bare clone + default worktree)
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

	// Step 2: Provision branch worktree from existing bare
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")
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

	inst, err := p.Provision(store, featureSpec)
	if err != nil {
		t.Fatalf("worktree provision failed: %v", err)
	}

	// AC: Second provision creates .worktrees/<name>/
	if !worktreeExists(featureRoot) {
		t.Fatal(".worktrees/feature-vpc/ worktree was not created")
	}
	if inst.State != StateReady {
		t.Fatalf("expected ready, got %s", inst.State)
	}
	if inst.Status != "SYNCED" {
		t.Fatalf("expected SYNCED, got %s", inst.Status)
	}

	// Verify .git in feature worktree is a file
	gitPath := filepath.Join(featureRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("stat .git: %v", err)
	}
	if info.IsDir() {
		t.Fatal(".git in worktree should be a file, not a directory")
	}

	// Both workspaces should be in the store
	if store.instances["myrepo"] == nil {
		t.Fatal("default workspace not in store")
	}
	if store.instances["myrepo/feature-vpc"] == nil {
		t.Fatal("feature workspace not in store")
	}
}

func TestProvision_NonDefaultBeforeBareClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	// AC: Non-default worktree requested before bare clone exists -> bare clone is created automatically
	spec := WorkspaceSpec{
		Name:         "myrepo/feature-vpc",
		VCS:          VCSTarget{Host: "github.com", CloneURL: upstreamBare, Branch: "feature-vpc"},
		ProjectRoot:  featureRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "feature-vpc",
		IsDefault:    false,
		Owner:        currentUser(),
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	if !dirExists(bareRoot) {
		t.Fatal(".bare/ should have been created automatically for non-default worktree")
	}
	if !worktreeExists(featureRoot) {
		t.Fatal("worktree should exist")
	}
	if inst.State != StateReady {
		t.Fatalf("expected ready, got %s", inst.State)
	}
}

// TestFullWorktreeLifecycle exercises the entire validation sequence from the
// validate-worktree-on-dev-pod story: provision bare clone + default, add branch
// worktree, inspect, deprovision clean, deprovision dirty (guard + force),
// cannot-delete-default guard, prune, sync, and idempotent re-provision.
func TestFullWorktreeLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "experiment", "exp.txt", "# experiment\n")
	addUpstreamBranch(t, dir, "spike-a", "a.tf", "# a\n")
	addUpstreamBranch(t, dir, "spike-b", "b.tf", "# b\n")

	repoRoot := filepath.Join(dir, "code", "gitlab.com", "org", "ocr-service")
	bareRoot := filepath.Join(repoRoot, ".bare")
	defaultRoot := filepath.Join(repoRoot, "default")
	experimentRoot := filepath.Join(repoRoot, ".worktrees", "experiment")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	// --- Step 3: Provision first workspace (bare clone + default worktree) ---
	defaultSpec := WorkspaceSpec{
		Name:         "ocr-service",
		VCS:          VCSTarget{Host: "gitlab.com", AuthUser: "oauth2", Repo: "org/ocr-service", Branch: "main", CloneURL: upstreamBare},
		PatName:      "gitlab-token",
		ProjectRoot:  defaultRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}
	inst, err := p.Provision(store, defaultSpec)
	if err != nil {
		t.Fatalf("step 3: default provision failed: %v", err)
	}
	if inst.State != StateReady {
		t.Fatalf("step 3: expected ready, got %s", inst.State)
	}
	if inst.Status != "SYNCED" {
		t.Fatalf("step 3: expected SYNCED, got %s", inst.Status)
	}

	// --- Step 4: Verify bare clone structure ---
	if !dirExists(bareRoot) {
		t.Fatal("step 4: .bare/ not created")
	}
	gitPath := filepath.Join(defaultRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		t.Fatalf("step 4: stat .git: %v", err)
	}
	if info.IsDir() {
		t.Fatal("step 4: .git in worktree should be a file, not directory")
	}
	// Verify default branch resolved via symbolic-ref
	branch, err := resolveDefaultBranch(bareRoot, currentUser())
	if err != nil {
		t.Fatalf("step 4: resolveDefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("step 4: expected main, got %s", branch)
	}

	// --- Step 5: Provision branch worktree (reuses existing .bare/) ---
	expSpec := WorkspaceSpec{
		Name:         "ocr-service/experiment",
		VCS:          VCSTarget{Host: "gitlab.com", AuthUser: "oauth2", Repo: "org/ocr-service", Branch: "experiment", CloneURL: upstreamBare},
		PatName:      "gitlab-token",
		ProjectRoot:  experimentRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "experiment",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	inst2, err := p.Provision(store, expSpec)
	if err != nil {
		t.Fatalf("step 5: experiment provision failed: %v", err)
	}
	if inst2.State != StateReady {
		t.Fatalf("step 5: expected ready, got %s", inst2.State)
	}
	// Verify .git file in experiment worktree
	expGit := filepath.Join(experimentRoot, ".git")
	expInfo, err := os.Stat(expGit)
	if err != nil {
		t.Fatalf("step 5: stat experiment .git: %v", err)
	}
	if expInfo.IsDir() {
		t.Fatal("step 5: experiment .git should be a file")
	}

	// --- Step 8: Inspect with worktree statistics ---
	worktrees, err := ListWorktrees(bareRoot, currentUser())
	if err != nil {
		t.Fatalf("step 8: ListWorktrees: %v", err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("step 8: expected 2 worktrees, got %d: %v", len(worktrees), worktrees)
	}

	// --- Step 10: Delete clean worktree ---
	result, err := p.Deprovision(store, "ocr-service/experiment", false)
	if err != nil {
		t.Fatalf("step 10: deprovision clean failed: %v", err)
	}
	if len(result.Removed) != 1 || result.Removed[0] != "ocr-service/experiment" {
		t.Fatalf("step 10: unexpected removed: %v", result.Removed)
	}
	if store.instances["ocr-service/experiment"] != nil {
		t.Fatal("step 10: experiment should be gone from state")
	}
	if worktreeExists(experimentRoot) {
		t.Fatal("step 10: experiment directory should be gone")
	}

	// --- Step 11: Re-create, dirty it, attempt delete (guard) ---
	_, err = p.Provision(store, expSpec)
	if err != nil {
		t.Fatalf("step 11: re-provision failed: %v", err)
	}
	os.WriteFile(filepath.Join(experimentRoot, "DIRTY.txt"), []byte("dirty\n"), 0644)
	_, err = p.Deprovision(store, "ocr-service/experiment", false)
	if err == nil {
		t.Fatal("step 11: expected error for dirty worktree")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("step 11: expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrWorktreeDirty {
		t.Fatalf("step 11: expected WORKTREE_DIRTY, got %s", pe.Code)
	}
	if store.instances["ocr-service/experiment"] == nil {
		t.Fatal("step 11: experiment should still be in state")
	}

	// --- Step 12: Force delete dirty worktree ---
	result, err = p.Deprovision(store, "ocr-service/experiment", true)
	if err != nil {
		t.Fatalf("step 12: force deprovision failed: %v", err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("step 12: unexpected removed count: %d", len(result.Removed))
	}
	if store.instances["ocr-service/experiment"] != nil {
		t.Fatal("step 12: experiment should be gone after force delete")
	}

	// --- Step 13: Delete default worktree (should be refused) ---
	_, err = p.Deprovision(store, "ocr-service", false)
	if err == nil {
		t.Fatal("step 13: expected error for default worktree delete")
	}
	pe, ok = err.(*ProvisionError)
	if !ok {
		t.Fatalf("step 13: expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrCannotDeleteDefault {
		t.Fatalf("step 13: expected CANNOT_DELETE_DEFAULT, got %s", pe.Code)
	}

	// --- Step 14-15: Provision multiple worktrees, prune clean ---
	spikeARoot := filepath.Join(repoRoot, ".worktrees", "spike-a")
	spikeBRoot := filepath.Join(repoRoot, ".worktrees", "spike-b")
	spikeASpec := WorkspaceSpec{
		Name:         "ocr-service/spike-a",
		VCS:          VCSTarget{Host: "gitlab.com", CloneURL: upstreamBare, Branch: "spike-a"},
		ProjectRoot:  spikeARoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-a",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	spikeBSpec := WorkspaceSpec{
		Name:         "ocr-service/spike-b",
		VCS:          VCSTarget{Host: "gitlab.com", CloneURL: upstreamBare, Branch: "spike-b"},
		ProjectRoot:  spikeBRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "spike-b",
		IsDefault:    false,
		Owner:        currentUser(),
	}
	_, err = p.Provision(store, spikeASpec)
	if err != nil {
		t.Fatalf("step 14: spike-a provision failed: %v", err)
	}
	_, err = p.Provision(store, spikeBSpec)
	if err != nil {
		t.Fatalf("step 14: spike-b provision failed: %v", err)
	}

	pruneResult, err := p.Prune(store, "ocr-service")
	if err != nil {
		t.Fatalf("step 15: prune failed: %v", err)
	}
	if len(pruneResult.Pruned) != 2 {
		t.Fatalf("step 15: expected 2 pruned, got %d: %v", len(pruneResult.Pruned), pruneResult.Pruned)
	}
	if len(pruneResult.Skipped) != 0 {
		t.Fatalf("step 15: expected 0 skipped, got %d", len(pruneResult.Skipped))
	}
	if store.instances["ocr-service"] == nil {
		t.Fatal("step 15: default should still exist after prune")
	}

	// --- Step 16: Prune with one dirty worktree ---
	_, _ = p.Provision(store, spikeASpec)
	_, _ = p.Provision(store, spikeBSpec)
	os.WriteFile(filepath.Join(spikeARoot, "DIRTY.txt"), []byte("dirty\n"), 0644)

	pruneResult, err = p.Prune(store, "ocr-service")
	if err != nil {
		t.Fatalf("step 16: prune failed: %v", err)
	}
	if len(pruneResult.Pruned) != 1 {
		t.Fatalf("step 16: expected 1 pruned, got %d: %v", len(pruneResult.Pruned), pruneResult.Pruned)
	}
	if len(pruneResult.Skipped) != 1 {
		t.Fatalf("step 16: expected 1 skipped, got %d", len(pruneResult.Skipped))
	}
	if pruneResult.Skipped[0].Reason != "uncommitted changes" {
		t.Fatalf("step 16: expected 'uncommitted changes', got %q", pruneResult.Skipped[0].Reason)
	}

	// Cleanup spike-a for next steps
	_, _ = p.Deprovision(store, "ocr-service/spike-a", true)

	// --- Step 17: Sync detects corrupted state ---
	store.instances["ocr-service"].State = StatePending // manually corrupt
	syncer := NewSyncer(store, filepath.Join(dir, "logs"))
	report, err := syncer.Sync()
	if err != nil {
		t.Fatalf("step 17: sync failed: %v", err)
	}
	if store.instances["ocr-service"].State != StateReady {
		t.Fatalf("step 17: expected ready after sync, got %s", store.instances["ocr-service"].State)
	}
	if len(report.StateChanges) == 0 {
		t.Fatal("step 17: expected state changes in sync report")
	}
	if store.instances["ocr-service"].LastSyncedAt == nil {
		t.Fatal("step 17: expected last_synced_at to be set")
	}

	// --- Step 19: Idempotent re-provision ---
	inst3, err := p.Provision(store, defaultSpec)
	if err != nil {
		t.Fatalf("step 19: idempotent re-provision failed: %v", err)
	}
	if inst3.State != StateReady {
		t.Fatalf("step 19: expected ready on re-provision, got %s", inst3.State)
	}

	// --- Verify response schema version is v2 ---
	resp := OkResponse("workspace.inspect", inst3)
	if resp.Version != "v2" {
		t.Fatalf("response version should be v2, got %s", resp.Version)
	}
}

func TestResolveDefaultBranch_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	// Clone bare locally
	bareRepo := filepath.Join(dir, "test.bare")
	runGit(t, "", "clone", "--bare", upstreamBare, bareRepo)

	// AC: Default branch resolved via git symbolic-ref, not hardcoded
	branch, err := resolveDefaultBranch(bareRepo, currentUser())
	if err != nil {
		t.Fatalf("resolveDefaultBranch failed: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected main, got %s", branch)
	}
}

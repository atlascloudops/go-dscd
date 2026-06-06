package domain

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type memStore struct {
	instances map[string]*Workspace
	locked    bool
}

func newMemStore() *memStore {
	return &memStore{instances: make(map[string]*Workspace)}
}

func (m *memStore) Load() (map[string]*Workspace, error) {
	return m.instances, nil
}

func (m *memStore) Save(instances map[string]*Workspace) error {
	m.instances = instances
	return nil
}

func (m *memStore) LoadState() (*DaemonState, error) {
	return &DaemonState{
		Workspaces:  m.instances,
		Credentials: make(map[string]*CredentialState),
	}, nil
}

func (m *memStore) SaveState(state *DaemonState) error {
	m.instances = state.Workspaces
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

// --- Name validation tests ---

func TestValidateName_ValidNames(t *testing.T) {
	validNames := []string{
		"tango",
		"my-project",
		"repo/feature-x",
		"beta",
		"beta/feature-vpc",
		"ocr-service",
		"ocr-service/experiment",
		"my_workspace",
		"workspace.v2",
		"a",
	}
	for _, name := range validNames {
		if err := validateName(name); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", name, err)
		}
	}
}

func TestValidateName_TooLong(t *testing.T) {
	name := strings.Repeat("a", 129)
	err := validateName(name)
	if err == nil {
		t.Fatal("expected error for name exceeding 128 characters")
	}
	pe := err.(*ProvisionError)
	if pe.Code != ErrSpecInvalid {
		t.Fatalf("expected SPEC_INVALID, got %s", pe.Code)
	}
	if !strings.Contains(pe.Message, "too long") {
		t.Fatalf("expected 'too long' in message, got %q", pe.Message)
	}
}

func TestValidateName_MaxLengthOk(t *testing.T) {
	name := strings.Repeat("a", 128)
	if err := validateName(name); err != nil {
		t.Fatalf("expected 128-char name to be valid, got: %v", err)
	}
}

func TestValidateName_TooManySegments(t *testing.T) {
	err := validateName("a/b/c")
	if err == nil {
		t.Fatal("expected error for name with too many path segments")
	}
	pe := err.(*ProvisionError)
	if pe.Code != ErrSpecInvalid {
		t.Fatalf("expected SPEC_INVALID, got %s", pe.Code)
	}
	if !strings.Contains(pe.Message, "too many path segments") {
		t.Fatalf("expected 'too many path segments' in message, got %q", pe.Message)
	}
}

func TestValidateName_PathTraversal(t *testing.T) {
	traversalNames := []string{
		"..",
		"../etc",
		"repo/..",
		".",
		"repo/.",
	}
	for _, name := range traversalNames {
		err := validateName(name)
		if err == nil {
			t.Errorf("expected error for path traversal name %q", name)
			continue
		}
		pe := err.(*ProvisionError)
		if pe.Code != ErrSpecInvalid {
			t.Errorf("expected SPEC_INVALID for %q, got %s", name, pe.Code)
		}
	}
}

func TestValidateName_EmptySegment(t *testing.T) {
	emptySegmentNames := []string{
		"/leading",
		"trailing/",
		"a//b",
	}
	for _, name := range emptySegmentNames {
		err := validateName(name)
		if err == nil {
			t.Errorf("expected error for empty segment in %q", name)
			continue
		}
		pe := err.(*ProvisionError)
		if pe.Code != ErrSpecInvalid {
			t.Errorf("expected SPEC_INVALID for %q, got %s", name, pe.Code)
		}
	}
}

func TestValidateName_UnsafeCharacters(t *testing.T) {
	unsafeNames := []string{
		"name\x00null",
		"name;injection",
		"name|pipe",
		"name&bg",
		"name`tick`",
		"name$var",
		"name\"quote",
		"name'squote",
		"name<angle",
		"name>angle",
		"name:colon",
		"name*glob",
		"name?wildcard",
		"name\\backslash",
		"name!bang",
		"name#hash",
		"name\nnewline",
		"name\ttab",
	}
	for _, name := range unsafeNames {
		err := validateName(name)
		if err == nil {
			t.Errorf("expected error for unsafe character in %q", name)
			continue
		}
		pe := err.(*ProvisionError)
		if pe.Code != ErrSpecInvalid {
			t.Errorf("expected SPEC_INVALID for %q, got %s", name, pe.Code)
		}
		if !strings.Contains(pe.Message, "unsafe character") {
			t.Errorf("expected 'unsafe character' in message for %q, got %q", name, pe.Message)
		}
	}
}

func TestValidateName_AcceptanceCriteria(t *testing.T) {
	// From the story acceptance criteria:
	// "tango" passes
	if err := validateName("tango"); err != nil {
		t.Errorf("expected 'tango' to pass: %v", err)
	}
	// "my-project" passes
	if err := validateName("my-project"); err != nil {
		t.Errorf("expected 'my-project' to pass: %v", err)
	}
	// "repo/feature-x" passes (worktree notation)
	if err := validateName("repo/feature-x"); err != nil {
		t.Errorf("expected 'repo/feature-x' to pass: %v", err)
	}
	// "a/b/c" fails (too many segments)
	if err := validateName("a/b/c"); err == nil {
		t.Error("expected 'a/b/c' to fail")
	}
	// "../etc" fails (path traversal)
	if err := validateName("../etc"); err == nil {
		t.Error("expected '../etc' to fail")
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
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}

	saved := store.instances["test"]
	if saved == nil {
		t.Fatal("workspace not persisted")
	}
	if saved.Status != StatusReady {
		t.Fatalf("persisted lifecycle should be ready, got %s", saved.Status)
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
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
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
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
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
	if inst2.Status != StatusReady {
		t.Fatalf("expected ready on re-provision, got %s", inst2.Status)
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
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
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
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
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
	if inst.Status != StatusReady {
		t.Fatalf("step 3: expected ready, got %s", inst.Status)
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
	if inst2.Status != StatusReady {
		t.Fatalf("step 5: expected ready, got %s", inst2.Status)
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

	// --- Step 17: Sync detects corrupted lifecycle ---
	store.instances["ocr-service"].Status = StatusPending // manually corrupt
	syncer := NewSyncer(store, nil)
	report, err := syncer.Sync()
	if err != nil {
		t.Fatalf("step 17: sync failed: %v", err)
	}
	if store.instances["ocr-service"].Status != StatusReady {
		t.Fatalf("step 17: expected ready after sync, got %s", store.instances["ocr-service"].Status)
	}
	if len(report.LifecycleChanges) == 0 {
		t.Fatal("step 17: expected lifecycle changes in sync report")
	}
	if store.instances["ocr-service"].LastSyncedAt == nil {
		t.Fatal("step 17: expected last_synced_at to be set")
	}

	// --- Step 19: Idempotent re-provision ---
	inst3, err := p.Provision(store, defaultSpec)
	if err != nil {
		t.Fatalf("step 19: idempotent re-provision failed: %v", err)
	}
	if inst3.Status != StatusReady {
		t.Fatalf("step 19: expected ready on re-provision, got %s", inst3.Status)
	}

	// --- Verify response schema version is v2 ---
	resp := OkResponse("workspace.inspect", inst3)
	if resp.Version != "v2" {
		t.Fatalf("response version should be v2, got %s", resp.Version)
	}
}

// pushUpstreamCommit adds a new commit to upstream on the given branch.
func pushUpstreamCommit(t *testing.T, dir, branch, filename, content string) {
	t.Helper()
	scratchClone := filepath.Join(dir, "scratch")
	runGit(t, scratchClone, "checkout", branch)
	os.WriteFile(filepath.Join(scratchClone, filename), []byte(content), 0644)
	runGit(t, scratchClone, "add", ".")
	runGit(t, scratchClone, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "update "+filename)
	runGit(t, scratchClone, "push", "origin", branch)
}

func TestHydrate_IdempotentProvisionFetchesAndPulls(t *testing.T) {
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

	// Step 1: Initial provision
	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("initial provision failed: %v", err)
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}
	initialCommit := inst.HeadCommit

	// Step 2: Push a new commit upstream
	pushUpstreamCommit(t, dir, "main", "update.txt", "new content\n")

	// Step 3: Re-provision (idempotent path) — should hydrate and pull the new commit
	inst2, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("idempotent provision failed: %v", err)
	}
	if inst2.Status != StatusReady {
		t.Fatalf("expected ready after hydrate, got %s", inst2.Status)
	}

	// Verify HEAD advanced
	if inst2.HeadCommit == initialCommit {
		t.Fatal("expected HEAD to advance after hydration, but it stayed the same")
	}
	if inst2.HeadCommit == "" {
		t.Fatal("expected non-empty head commit after hydration")
	}

	// Verify hydrate events are present
	hasHydrateStarted := false
	hasHydrateCompleted := false
	for _, ev := range inst2.Events {
		if ev.Event == string(EventHydrateStarted) {
			hasHydrateStarted = true
		}
		if ev.Event == string(EventHydrateCompleted) {
			hasHydrateCompleted = true
		}
	}
	if !hasHydrateStarted {
		t.Error("expected hydrate_started event")
	}
	if !hasHydrateCompleted {
		t.Error("expected hydrate_completed event")
	}
}

func TestHydrate_DirtyWorktreeSkipped(t *testing.T) {
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

	// Initial provision
	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("initial provision failed: %v", err)
	}

	// Dirty the worktree
	os.WriteFile(filepath.Join(projectRoot, "DIRTY.txt"), []byte("dirty\n"), 0644)

	// Push upstream change
	pushUpstreamCommit(t, dir, "main", "update.txt", "new content\n")

	// Re-provision — hydration should skip the dirty worktree
	inst2, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("idempotent provision failed: %v", err)
	}
	if inst2.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst2.Status)
	}

	// Verify hydrate_skipped event with "uncommitted changes" detail
	hasSkipped := false
	for _, ev := range inst2.Events {
		if ev.Event == string(EventHydrateSkipped) && strings.Contains(ev.Detail, "uncommitted changes") {
			hasSkipped = true
		}
	}
	if !hasSkipped {
		t.Error("expected hydrate_skipped event with 'uncommitted changes' detail")
	}
}

func TestHydrate_DivergedBranchSkipped(t *testing.T) {
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

	// Initial provision
	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("initial provision failed: %v", err)
	}

	// Create a local commit that diverges from upstream
	runGit(t, projectRoot, "-c", "user.name=Test", "-c", "user.email=t@t.com",
		"commit", "--allow-empty", "-m", "local diverged commit")

	// Push a different commit upstream
	pushUpstreamCommit(t, dir, "main", "upstream-only.txt", "upstream\n")

	// Re-provision — hydration should skip due to divergence
	inst2, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("idempotent provision failed: %v", err)
	}
	if inst2.Status != StatusReady {
		t.Fatalf("expected ready even with diverged branch, got %s", inst2.Status)
	}

	// Verify hydrate_skipped event with divergence detail
	hasSkipped := false
	for _, ev := range inst2.Events {
		if ev.Event == string(EventHydrateSkipped) && strings.Contains(ev.Detail, "diverged") {
			hasSkipped = true
		}
	}
	if !hasSkipped {
		t.Error("expected hydrate_skipped event with divergence detail")
	}
}

func TestHydrate_UnrelatedBranchNotTouched(t *testing.T) {
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
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

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

	// Record feature worktree HEAD before re-provision of default
	featureHead := ResolveHeadCommit(featureRoot, currentUser())

	// Push commits to both branches
	pushUpstreamCommit(t, dir, "main", "main-update.txt", "main update\n")
	// Switch back to main in scratch before pushing to feature
	scratchClone := filepath.Join(dir, "scratch")
	runGit(t, scratchClone, "checkout", "feature-vpc")
	os.WriteFile(filepath.Join(scratchClone, "vpc-update.tf"), []byte("# vpc update\n"), 0644)
	runGit(t, scratchClone, "add", ".")
	runGit(t, scratchClone, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "vpc update")
	runGit(t, scratchClone, "push", "origin", "feature-vpc")

	// Re-provision default — hydration should pull main but NOT touch feature-vpc
	inst, err := p.Provision(store, defaultSpec)
	if err != nil {
		t.Fatalf("re-provision failed: %v", err)
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}

	// Feature worktree HEAD should be unchanged
	featureHeadAfter := ResolveHeadCommit(featureRoot, currentUser())
	if featureHeadAfter != featureHead {
		t.Errorf("feature worktree HEAD changed during default hydration: %s -> %s", featureHead, featureHeadAfter)
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

// --- Template provisioning tests ---

// createTemplateRepo creates a local bare repo with template content (README + template files),
// suitable as a template source for tests. Returns the path to the bare upstream.
func createTemplateRepo(t *testing.T, dir string) string {
	t.Helper()
	upstreamBare := filepath.Join(dir, "template-upstream.git")
	runGit(t, "", "init", "--bare", upstreamBare)

	scratchClone := filepath.Join(dir, "template-scratch")
	runGit(t, "", "clone", upstreamBare, scratchClone)
	os.WriteFile(filepath.Join(scratchClone, "README.md"), []byte("# Template Project\n"), 0644)
	os.WriteFile(filepath.Join(scratchClone, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644)
	os.MkdirAll(filepath.Join(scratchClone, "pkg"), 0755)
	os.WriteFile(filepath.Join(scratchClone, "pkg", "lib.go"), []byte("package pkg\n"), 0644)
	runGit(t, scratchClone, "add", ".")
	runGit(t, scratchClone, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "template init")
	runGit(t, scratchClone, "push", "origin", "main")
	return upstreamBare
}

func TestProvisionTemplate_BasicFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "my-project")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "my-project",
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		Template: &TemplateSource{
			CloneURL: templateBare,
			Host:     "github.com",
			Repo:     "org/my-template",
		},
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("template provision failed: %v", err)
	}

	// AC: Resulting workspace has bare+worktree structure identical to standard clones
	if !dirExists(bareRoot) {
		t.Fatal(".bare/ directory was not created")
	}
	if !worktreeExists(projectRoot) {
		t.Fatal("default/ worktree was not created")
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
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

	// AC: Template files are present in the worktree
	if _, err := os.Stat(filepath.Join(projectRoot, "README.md")); err != nil {
		t.Fatal("README.md not found in worktree")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "main.go")); err != nil {
		t.Fatal("main.go not found in worktree")
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "pkg", "lib.go")); err != nil {
		t.Fatal("pkg/lib.go not found in worktree")
	}

	// AC: Initial commit message includes the template repo slug
	commitMsg := getHeadCommitMessage(t, projectRoot)
	if !strings.Contains(commitMsg, "org/my-template") {
		t.Fatalf("expected commit message to contain template repo slug, got %q", commitMsg)
	}

	// AC: head commit is set
	if inst.HeadCommit == "" {
		t.Fatal("expected non-empty head commit")
	}
}

func TestProvisionTemplate_TemplateRemoteFetchOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "my-project")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "my-project",
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		Template: &TemplateSource{
			CloneURL: templateBare,
			Host:     "github.com",
			Repo:     "org/my-template",
		},
	}

	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("template provision failed: %v", err)
	}

	// AC: template remote is configured as fetch-only (push URL is "no_push")
	templateURL := getRemoteURL(t, bareRoot, "template")
	if templateURL != templateBare {
		t.Fatalf("expected template remote URL %q, got %q", templateBare, templateURL)
	}

	pushURL := getRemotePushURL(t, bareRoot, "template")
	if pushURL != "no_push" {
		t.Fatalf("expected template push URL 'no_push', got %q", pushURL)
	}
}

func TestProvisionTemplate_OriginWhenVCSCloneURLSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "my-project")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	originURL := "https://github.com/org/my-project.git"
	spec := WorkspaceSpec{
		Name:         "my-project",
		VCS:          VCSTarget{CloneURL: originURL, Host: "github.com"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		Template: &TemplateSource{
			CloneURL: templateBare,
			Host:     "github.com",
			Repo:     "org/my-template",
		},
	}

	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("template provision failed: %v", err)
	}

	// AC: origin remote is configured when VCS.CloneURL is non-empty
	remoteURL := getRemoteURL(t, bareRoot, "origin")
	if remoteURL != originURL {
		t.Fatalf("expected origin URL %q, got %q", originURL, remoteURL)
	}
}

func TestProvisionTemplate_NoOriginWhenVCSCloneURLEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "my-project")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "my-project",
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		Template: &TemplateSource{
			CloneURL: templateBare,
			Host:     "github.com",
			Repo:     "org/my-template",
		},
	}

	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("template provision failed: %v", err)
	}

	// AC: origin remote is NOT configured when VCS.CloneURL is empty
	cmd := exec.Command("git", "-C", bareRoot, "remote", "get-url", "origin")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected origin remote to not exist when VCS.CloneURL is empty")
	}
}

func TestProvisionTemplate_Events(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "my-project")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "my-project",
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		Template: &TemplateSource{
			CloneURL: templateBare,
			Host:     "github.com",
			Repo:     "org/my-template",
		},
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("template provision failed: %v", err)
	}

	// AC: New events appear in the event stream in correct order
	expectedEvents := []WorkspaceEvent{
		EventTemplateCloneStarted,
		EventTemplateCloneCompleted,
		EventTemplateReinitCompleted,
		EventWorktreeCreated,
	}

	if len(inst.Events) < len(expectedEvents) {
		t.Fatalf("expected at least %d events, got %d", len(expectedEvents), len(inst.Events))
	}

	for i, expected := range expectedEvents {
		if inst.Events[i].Event != string(expected) {
			t.Errorf("event[%d]: expected %s, got %s", i, expected, inst.Events[i].Event)
		}
	}

	// Verify template_reinit_completed has the repo slug as detail
	for _, ev := range inst.Events {
		if ev.Event == string(EventTemplateReinitCompleted) {
			if ev.Detail != "org/my-template" {
				t.Errorf("expected template_reinit_completed detail to be 'org/my-template', got %q", ev.Detail)
			}
		}
	}
}

func TestProvisionTemplate_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "my-project")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "my-project",
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		Template: &TemplateSource{
			CloneURL: templateBare,
			Host:     "github.com",
			Repo:     "org/my-template",
		},
	}

	// First provision
	inst1, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("first provision failed: %v", err)
	}
	if inst1.Status != StatusReady {
		t.Fatalf("expected ready on first provision, got %s", inst1.Status)
	}

	// Second provision (idempotent) — should return ready without error
	inst2, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("idempotent provision failed: %v", err)
	}
	if inst2.Status != StatusReady {
		t.Fatalf("expected ready on idempotent provision, got %s", inst2.Status)
	}
}

func TestProvisionTemplate_InspectReturnsTemplateRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "my-project")
	bareRoot := filepath.Join(repoRoot, ".bare")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:         "my-project",
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     bareRoot,
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		Template: &TemplateSource{
			CloneURL: templateBare,
			Host:     "github.com",
			Repo:     "org/my-template",
		},
	}

	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// AC: dscd workspace inspect returns template_repo for template-created workspaces
	templateRepo := ResolveTemplateRepo(bareRoot, currentUser())
	if templateRepo != templateBare {
		t.Fatalf("expected template repo URL %q, got %q", templateBare, templateRepo)
	}
}

func TestResolveTemplateRepo_StandardClone(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "myrepo")
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

	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// AC: Standard clone path (no Template field) — ResolveTemplateRepo returns empty
	templateRepo := ResolveTemplateRepo(bareRoot, currentUser())
	if templateRepo != "" {
		t.Fatalf("expected empty template repo for standard clone, got %q", templateRepo)
	}
}

func TestValidateSpec_TemplateWithEmptyCloneURL(t *testing.T) {
	spec := WorkspaceSpec{
		Name:         "test",
		ProjectRoot:  "/tmp/test",
		RepoRoot:     "/tmp",
		BareRoot:     "/tmp/.bare",
		WorktreeName: "default",
		Owner:        "user",
		Template: &TemplateSource{
			CloneURL: "",
			Host:     "github.com",
			Repo:     "org/tmpl",
		},
	}
	err := validateSpec(spec)
	if err == nil {
		t.Fatal("expected error for template with empty clone_url")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrSpecInvalid {
		t.Fatalf("expected SPEC_INVALID, got %s", pe.Code)
	}
	if !strings.Contains(pe.Message, "template.clone_url") {
		t.Fatalf("expected message to mention template.clone_url, got %q", pe.Message)
	}
}

func TestValidateSpec_TemplateWithoutVCSCloneURL(t *testing.T) {
	// AC: Spec validation accepts empty VCS.CloneURL when Template is present
	spec := WorkspaceSpec{
		Name:         "test",
		ProjectRoot:  "/tmp/test",
		RepoRoot:     "/tmp",
		BareRoot:     "/tmp/.bare",
		WorktreeName: "default",
		Owner:        "user",
		Template: &TemplateSource{
			CloneURL: "https://github.com/org/tmpl.git",
			Host:     "github.com",
			Repo:     "org/tmpl",
		},
	}
	if err := validateSpec(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTemplateEvents_DoNotAffectStatus(t *testing.T) {
	// AC: Template events are informational and do not change workspace status
	inst := &Workspace{}
	inst.RecordEvent(EventTemplateCloneStarted, "url")
	if inst.Status != StatusPending {
		t.Fatalf("expected pending after template_clone_started, got %s", inst.Status)
	}

	inst.RecordEvent(EventTemplateCloneCompleted, "")
	if inst.Status != StatusPending {
		t.Fatalf("expected pending after template_clone_completed, got %s", inst.Status)
	}

	inst.RecordEvent(EventTemplateReinitCompleted, "org/tmpl")
	if inst.Status != StatusPending {
		t.Fatalf("expected pending after template_reinit_completed, got %s", inst.Status)
	}

	// Once worktree_created fires, status becomes Ready
	inst.RecordEvent(EventWorktreeCreated, "main")
	if inst.Status != StatusReady {
		t.Fatalf("expected ready after worktree_created, got %s", inst.Status)
	}
}

// --- Activity log integration tests ---

func TestProvision_ActivityLogReceivesWorkspaceEvents(t *testing.T) {
	dir := t.TempDir()
	upstream := createUpstreamRepo(t, dir)
	repoRoot := filepath.Join(dir, "ws")
	projectRoot := filepath.Join(repoRoot, "default")

	actLogPath := filepath.Join(dir, "activity.log")
	actLog := NewActivityLog(actLogPath)

	store := newMemStore()
	p := &Provisioner{
		LogDir:      filepath.Join(dir, "logs"),
		ActivityLog: actLog,
	}

	spec := WorkspaceSpec{
		Name:         "test",
		VCS:          VCSTarget{CloneURL: upstream, Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     filepath.Join(repoRoot, ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Workspace events should have workspace scope
	for _, ev := range inst.Events {
		if ev.Scope.Kind != ScopeKindWorkspace {
			t.Errorf("expected workspace scope, got %s for event %s", ev.Scope.Kind, ev.Event)
		}
		if ev.Scope.Name != "test" {
			t.Errorf("expected scope name 'test', got %s for event %s", ev.Scope.Name, ev.Event)
		}
	}

	// Activity log should have received all workspace events
	records, err := actLog.Read(ActivityLogFilter{ScopeKind: ScopeKindWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 3 {
		t.Fatalf("expected at least 3 activity log records (clone_started, clone_completed, worktree_creating, worktree_created, hydrate_*), got %d", len(records))
	}

	// Verify clone_started is in the activity log
	foundCloneStarted := false
	for _, r := range records {
		if r.Event == string(EventCloneStarted) {
			foundCloneStarted = true
			break
		}
	}
	if !foundCloneStarted {
		t.Fatal("expected clone_started in activity log")
	}
}

func TestProvision_ActivityLogReceivesIDEEvents(t *testing.T) {
	dir := t.TempDir()
	upstream := createUpstreamRepo(t, dir)
	repoRoot := filepath.Join(dir, "ws")
	projectRoot := filepath.Join(repoRoot, "default")

	actLogPath := filepath.Join(dir, "activity.log")
	actLog := NewActivityLog(actLogPath)

	portFile := filepath.Join(dir, "ports.json")
	store := newMemStore()
	p := &Provisioner{
		LogDir:        filepath.Join(dir, "logs"),
		ActivityLog:   actLog,
		IDEAdapter:    &stubIDEAdapter{},
		PortAllocator: NewPortAllocator(portFile),
	}

	spec := WorkspaceSpec{
		Name:         "test",
		VCS:          VCSTarget{CloneURL: upstream, Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     filepath.Join(repoRoot, ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
		IDE:          &IDESpecConfig{Adapter: "stub"},
	}

	_, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Activity log should contain IDE events with correct scope
	ideRecords, err := actLog.Read(ActivityLogFilter{ScopeKind: ScopeKindIDE})
	if err != nil {
		t.Fatal(err)
	}
	if len(ideRecords) < 2 {
		t.Fatalf("expected at least 2 IDE activity log records (started, ready), got %d", len(ideRecords))
	}

	// Verify scope name matches workspace name
	for _, r := range ideRecords {
		if r.Scope.Name != "test" {
			t.Errorf("expected IDE scope name 'test', got %s", r.Scope.Name)
		}
	}
}

func TestProvision_NilActivityLogDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	upstream := createUpstreamRepo(t, dir)
	repoRoot := filepath.Join(dir, "ws")
	projectRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{
		LogDir: filepath.Join(dir, "logs"),
		// ActivityLog intentionally nil
	}

	spec := WorkspaceSpec{
		Name:         "test",
		VCS:          VCSTarget{CloneURL: upstream, Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     filepath.Join(repoRoot, ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        currentUser(),
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}
}

func TestProvision_EventRecordScopesCorrectOnIdempotent(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "repo")
	projectRoot := filepath.Join(repoRoot, "default")

	// Simulate existing worktree with .git directory
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	actLogPath := filepath.Join(dir, "activity.log")
	actLog := NewActivityLog(actLogPath)

	store := newMemStore()
	p := &Provisioner{
		LogDir:      filepath.Join(dir, "logs"),
		ActivityLog: actLog,
	}

	spec := WorkspaceSpec{
		Name:         "myws",
		VCS:          VCSTarget{Host: "github.com", CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot:  projectRoot,
		RepoRoot:     repoRoot,
		BareRoot:     filepath.Join(repoRoot, ".bare"),
		WorktreeName: "default",
		IsDefault:    true,
		Owner:        "testuser",
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The worktree_created event should carry workspace:myws scope
	if len(inst.Events) == 0 {
		t.Fatal("expected at least one event")
	}
	for _, ev := range inst.Events {
		if ev.Scope.Kind != ScopeKindWorkspace {
			t.Errorf("expected workspace scope, got %s for event %s", ev.Scope.Kind, ev.Event)
		}
		if ev.Scope.Name != "myws" {
			t.Errorf("expected scope name 'myws', got %s for event %s", ev.Scope.Name, ev.Event)
		}
	}

	// Activity log should have received the event
	records, err := actLog.Read(ActivityLogFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one activity log record")
	}
	if records[0].Scope.Name != "myws" {
		t.Fatalf("expected scope name 'myws' in activity log, got %s", records[0].Scope.Name)
	}
}

// --- Test helpers for template tests ---

func getHeadCommitMessage(t *testing.T, projectRoot string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", projectRoot, "log", "-1", "--format=%s")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func getRemoteURL(t *testing.T, bareRoot, remoteName string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", bareRoot, "remote", "get-url", remoteName)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git remote get-url %s failed: %v", remoteName, err)
	}
	return strings.TrimSpace(string(out))
}

func getRemotePushURL(t *testing.T, bareRoot, remoteName string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", bareRoot, "remote", "get-url", "--push", remoteName)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git remote get-url --push %s failed: %v", remoteName, err)
	}
	return strings.TrimSpace(string(out))
}

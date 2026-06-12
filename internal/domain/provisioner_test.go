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

// testWorkspace is a test helper that sets up workspace paths following the
// server-owned convention: <workspaceRoot>/<host>/<repo>/
type testWorkspace struct {
	WorkspaceRoot string // temp dir root
	RepoRoot      string // <workspaceRoot>/<host>/<repo>
	BareRoot      string // <repoRoot>/.bare
	DefaultRoot   string // <repoRoot>/default
}

// setupTestWorkspace creates a workspace layout for VCS-backed tests.
// host/repo are used to derive paths, matching what ProvisionParams methods derive.
func setupTestWorkspace(t *testing.T, host, repo string) testWorkspace {
	t.Helper()
	dir := t.TempDir()
	repoRoot := DeriveRepoRoot(dir, host, repo)
	return testWorkspace{
		WorkspaceRoot: dir,
		RepoRoot:      repoRoot,
		BareRoot:      DeriveBareRoot(repoRoot),
		DefaultRoot:   DeriveProjectRoot(repoRoot, "default"),
	}
}

// setupTestLocalWorkspace creates a workspace layout for template-only tests.
func setupTestLocalWorkspace(t *testing.T, name string) testWorkspace {
	t.Helper()
	dir := t.TempDir()
	repoRoot := DeriveLocalRepoRoot(dir, name)
	return testWorkspace{
		WorkspaceRoot: dir,
		RepoRoot:      repoRoot,
		BareRoot:      DeriveBareRoot(repoRoot),
		DefaultRoot:   DeriveProjectRoot(repoRoot, "default"),
	}
}

// worktreeRoot returns the path for a named worktree under this workspace.
func (tw testWorkspace) worktreeRoot(name string) string {
	return DeriveProjectRoot(tw.RepoRoot, name)
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
	for _, field := range []string{"name", "vcs.clone_url", "owner"} {
		if !strings.Contains(pe.Detail, field) {
			t.Fatalf("expected detail to contain %q, got %q", field, pe.Detail)
		}
	}
}

func TestValidateSpec_Valid(t *testing.T) {
	spec := WorkspaceSpec{
		Name:  "test",
		VCS:   VCSTarget{CloneURL: "https://github.com/org/repo.git"},
		Owner: "user",
	}
	if err := validateSpec(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSpec_MissingOwner(t *testing.T) {
	spec := WorkspaceSpec{
		Name: "test",
		VCS:  VCSTarget{CloneURL: "https://github.com/org/repo.git"},
	}
	err := validateSpec(spec)
	if err == nil {
		t.Fatal("expected error when owner is empty")
	}
	pe := err.(*ProvisionError)
	if !strings.Contains(pe.Detail, "owner") {
		t.Fatalf("expected detail to mention owner, got %q", pe.Detail)
	}
}

func TestValidateSpec_MissingName(t *testing.T) {
	spec := WorkspaceSpec{
		VCS:   VCSTarget{CloneURL: "https://github.com/org/repo.git"},
		Owner: "user",
	}
	err := validateSpec(spec)
	if err == nil {
		t.Fatal("expected error when name is empty")
	}
	pe := err.(*ProvisionError)
	if !strings.Contains(pe.Detail, "name") {
		t.Fatalf("expected detail to mention name, got %q", pe.Detail)
	}
}

func TestValidateSpec_MissingCloneURL(t *testing.T) {
	spec := WorkspaceSpec{
		Name:  "test",
		Owner: "user",
	}
	err := validateSpec(spec)
	if err == nil {
		t.Fatal("expected error when vcs.clone_url is empty")
	}
	pe := err.(*ProvisionError)
	if !strings.Contains(pe.Detail, "vcs.clone_url") {
		t.Fatalf("expected detail to mention vcs.clone_url, got %q", pe.Detail)
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
	tw := setupTestWorkspace(t, "github.com", "org/repo")

	// Simulate existing worktree with .git directory
	os.MkdirAll(filepath.Join(tw.DefaultRoot, ".git"), 0755)

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "test",
			VCS:   VCSTarget{Host: "github.com", Repo: "org/repo", CloneURL: "https://github.com/org/repo.git"},
			Owner: "testuser",
		},
		WorkspaceRoot: tw.WorkspaceRoot,
	}

	inst, err := p.Provision(store, params)
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
	tw := setupTestWorkspace(t, "github.com", "org/repo")

	// Simulate existing default worktree with .git as a file (worktree pointer)
	os.MkdirAll(tw.DefaultRoot, 0755)
	os.WriteFile(filepath.Join(tw.DefaultRoot, ".git"), []byte("gitdir: ../../.bare/worktrees/default\n"), 0644)

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "test",
			VCS:   VCSTarget{Host: "github.com", Repo: "org/repo", CloneURL: "https://github.com/org/repo.git"},
			Owner: "testuser",
		},
		WorkspaceRoot: tw.WorkspaceRoot,
	}

	inst, err := p.Provision(store, params)
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

	_, err := p.Provision(store, ProvisionParams{})
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	bareRoot := params.BareRoot()
	projectRoot := params.ProjectRoot()

	inst, err := p.Provision(store, params)
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
	if inst.DefaultWorktree() == nil || inst.DefaultWorktree().HeadCommit == "" {
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
	inst2, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("idempotent provision failed: %v", err)
	}
	if inst2.Status != StatusReady {
		t.Fatalf("expected ready on re-provision, got %s", inst2.Status)
	}
}

func TestAddWorktree_FromExistingBare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feature-vpc", "vpc.tf", "# vpc\n")

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")

	store := newMemStore()
	p := &Provisioner{}

	// Step 1: Provision default (bare clone + default worktree)
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	_, err := p.Provision(store, defaultParams)
	if err != nil {
		t.Fatalf("default provision failed: %v", err)
	}

	// Step 2: Add branch worktree via AddWorktree (not provision)
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")
	result, err := p.AddWorktree(store, "myrepo", "feature-vpc")
	if err != nil {
		t.Fatalf("AddWorktree failed: %v", err)
	}

	// AC: AddWorktree creates .worktrees/<name>/
	if !worktreeExists(featureRoot) {
		t.Fatal(".worktrees/feature-vpc/ worktree was not created")
	}
	if !result.Created {
		t.Fatal("expected Created=true")
	}
	if result.ProjectRoot != featureRoot {
		t.Fatalf("expected project root %s, got %s", featureRoot, result.ProjectRoot)
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

	// Workspace should have 2 worktrees in the aggregate
	ws := store.instances["myrepo"]
	if ws == nil {
		t.Fatal("workspace not in store")
	}
	if len(ws.Worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(ws.Worktrees))
	}
}

func TestProvision_AlwaysCreatesDefaultWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)

	repoRoot := filepath.Join(dir, "code", "github.com", "test", "myrepo")
	defaultRoot := filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{}

	// Even if an old-format name with "/" is sent, provision always creates
	// the default worktree (backward compatibility — extra fields are ignored)
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	if !dirExists(params.BareRoot()) {
		t.Fatal(".bare/ should exist")
	}
	if !worktreeExists(defaultRoot) {
		t.Fatal("default worktree should exist")
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}

	// Verify default worktree is the only one
	if len(inst.Worktrees) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(inst.Worktrees))
	}
	if !inst.Worktrees[0].IsDefault {
		t.Fatal("expected worktree to be default")
	}
	if inst.Worktrees[0].Name != "default" {
		t.Fatalf("expected worktree name 'default', got %q", inst.Worktrees[0].Name)
	}
}

// TestFullWorktreeLifecycle exercises the entire validation sequence:
// provision bare clone + default, add branch worktree via AddWorktree, inspect,
// deprovision worktree, dirty guard + force, prune, sync, and idempotent re-provision.
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
	p := &Provisioner{}

	// --- Step 3: Provision workspace (bare clone + default worktree) ---
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:    "ocr-service",
			VCS:     VCSTarget{Host: "gitlab.com", Repo: "org/ocr-service", CloneURL: upstreamBare},
			PatName: "gitlab-token",
			Owner:   currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	inst, err := p.Provision(store, defaultParams)
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
	branch, err := resolveDefaultBranch(bareRoot, currentUser())
	if err != nil {
		t.Fatalf("step 4: resolveDefaultBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("step 4: expected main, got %s", branch)
	}

	// --- Step 5: Add branch worktree via AddWorktree ---
	expResult, err := p.AddWorktree(store, "ocr-service", "experiment")
	if err != nil {
		t.Fatalf("step 5: AddWorktree experiment failed: %v", err)
	}
	if !expResult.Created {
		t.Fatal("step 5: expected Created=true")
	}
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

	// --- Step 10: Delete clean worktree via DeprovisionWorktree ---
	dpResult, err := p.DeprovisionWorktree(store, "ocr-service", "experiment", false)
	if err != nil {
		t.Fatalf("step 10: deprovision worktree failed: %v", err)
	}
	if len(dpResult.RemovedWorktrees) != 1 || dpResult.RemovedWorktrees[0] != "experiment" {
		t.Fatalf("step 10: unexpected removed worktrees: %v", dpResult.RemovedWorktrees)
	}
	if worktreeExists(experimentRoot) {
		t.Fatal("step 10: experiment directory should be gone")
	}

	// --- Step 11: Re-create, dirty it, attempt delete (guard) ---
	_, err = p.AddWorktree(store, "ocr-service", "experiment")
	if err != nil {
		t.Fatalf("step 11: re-add worktree failed: %v", err)
	}
	os.WriteFile(filepath.Join(experimentRoot, "DIRTY.txt"), []byte("dirty\n"), 0644)
	_, err = p.DeprovisionWorktree(store, "ocr-service", "experiment", false)
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

	// --- Step 12: Force delete dirty worktree ---
	_, err = p.DeprovisionWorktree(store, "ocr-service", "experiment", true)
	if err != nil {
		t.Fatalf("step 12: force deprovision worktree failed: %v", err)
	}

	// --- Step 14-15: Add multiple worktrees via AddWorktree, prune clean ---
	spikeARoot := filepath.Join(repoRoot, ".worktrees", "spike-a")
	_, err = p.AddWorktree(store, "ocr-service", "spike-a")
	if err != nil {
		t.Fatalf("step 14: AddWorktree spike-a failed: %v", err)
	}
	_, err = p.AddWorktree(store, "ocr-service", "spike-b")
	if err != nil {
		t.Fatalf("step 14: AddWorktree spike-b failed: %v", err)
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
	_, err = p.AddWorktree(store, "ocr-service", "spike-a")
	if err != nil {
		t.Fatalf("step 16: AddWorktree spike-a failed: %v", err)
	}
	_, err = p.AddWorktree(store, "ocr-service", "spike-b")
	if err != nil {
		t.Fatalf("step 16: AddWorktree spike-b failed: %v", err)
	}
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
	inst3, err := p.Provision(store, defaultParams)
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
	_ = filepath.Join(repoRoot, ".bare")
	_ = filepath.Join(repoRoot, "default")

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	// Step 1: Initial provision
	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("initial provision failed: %v", err)
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}
	initialCommit := inst.DefaultWorktree().HeadCommit

	// Step 2: Push a new commit upstream
	pushUpstreamCommit(t, dir, "main", "update.txt", "new content\n")

	// Step 3: Re-provision (idempotent path) — should hydrate and pull the new commit
	inst2, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("idempotent provision failed: %v", err)
	}
	if inst2.Status != StatusReady {
		t.Fatalf("expected ready after hydrate, got %s", inst2.Status)
	}

	// Verify HEAD advanced
	if inst2.DefaultWorktree().HeadCommit == initialCommit {
		t.Fatal("expected HEAD to advance after hydration, but it stayed the same")
	}
	if inst2.DefaultWorktree().HeadCommit == "" {
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	projectRoot := params.ProjectRoot()

	// Initial provision
	_, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("initial provision failed: %v", err)
	}

	// Dirty the worktree
	os.WriteFile(filepath.Join(projectRoot, "DIRTY.txt"), []byte("dirty\n"), 0644)

	// Push upstream change
	pushUpstreamCommit(t, dir, "main", "update.txt", "new content\n")

	// Re-provision — hydration should skip the dirty worktree
	inst2, err := p.Provision(store, params)
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	projectRoot := params.ProjectRoot()

	// Initial provision
	_, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("initial provision failed: %v", err)
	}

	// Create a local commit that diverges from upstream
	runGit(t, projectRoot, "-c", "user.name=Test", "-c", "user.email=t@t.com",
		"commit", "--allow-empty", "-m", "local diverged commit")

	// Push a different commit upstream
	pushUpstreamCommit(t, dir, "main", "upstream-only.txt", "upstream\n")

	// Re-provision — hydration should skip due to divergence
	inst2, err := p.Provision(store, params)
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
	featureRoot := filepath.Join(repoRoot, ".worktrees", "feature-vpc")

	store := newMemStore()
	p := &Provisioner{}

	// Provision default
	defaultParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	_, err := p.Provision(store, defaultParams)
	if err != nil {
		t.Fatalf("default provision failed: %v", err)
	}

	// Provision feature worktree
	featureParams := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo/feature-vpc",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	_, err = p.Provision(store, featureParams)
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
	inst, err := p.Provision(store, defaultParams)
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

	wsRoot := filepath.Join(dir, "code")
	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "my-project",
			Owner: currentUser(),
			Template: &TemplateSource{
				CloneURL: templateBare,
				Host:     "github.com",
				Repo:     "org/my-template",
			},
		},
		WorkspaceRoot: wsRoot,
	}
	bareRoot := params.BareRoot()
	projectRoot := params.ProjectRoot()

	inst, err := p.Provision(store, params)
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
	if inst.DefaultWorktree() == nil || inst.DefaultWorktree().HeadCommit == "" {
		t.Fatal("expected non-empty head commit")
	}
}

func TestProvisionTemplate_TemplateRemoteFetchOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	templateBare := createTemplateRepo(t, dir)

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "my-project",
			Owner: currentUser(),
			Template: &TemplateSource{
				CloneURL: templateBare,
				Host:     "github.com",
				Repo:     "org/my-template",
			},
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	bareRoot := params.BareRoot()

	_, err := p.Provision(store, params)
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

	store := newMemStore()
	p := &Provisioner{}

	originURL := "https://github.com/org/my-project.git"
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name: "my-project",
			VCS:  VCSTarget{CloneURL: originURL, Host: "github.com"},
			Owner: currentUser(),
			Template: &TemplateSource{
				CloneURL: templateBare,
				Host:     "github.com",
				Repo:     "org/my-template",
			},
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	bareRoot := params.BareRoot()

	_, err := p.Provision(store, params)
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "my-project",
			Owner: currentUser(),
			Template: &TemplateSource{
				CloneURL: templateBare,
				Host:     "github.com",
				Repo:     "org/my-template",
			},
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	bareRoot := params.BareRoot()

	_, err := p.Provision(store, params)
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "my-project",
			Owner: currentUser(),
			Template: &TemplateSource{
				CloneURL: templateBare,
				Host:     "github.com",
				Repo:     "org/my-template",
			},
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	inst, err := p.Provision(store, params)
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "my-project",
			Owner: currentUser(),
			Template: &TemplateSource{
				CloneURL: templateBare,
				Host:     "github.com",
				Repo:     "org/my-template",
			},
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	// First provision
	inst1, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("first provision failed: %v", err)
	}
	if inst1.Status != StatusReady {
		t.Fatalf("expected ready on first provision, got %s", inst1.Status)
	}

	// Second provision (idempotent) — should return ready without error
	inst2, err := p.Provision(store, params)
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "my-project",
			Owner: currentUser(),
			Template: &TemplateSource{
				CloneURL: templateBare,
				Host:     "github.com",
				Repo:     "org/my-template",
			},
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	bareRoot := params.BareRoot()

	_, err := p.Provision(store, params)
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

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	bareRoot := params.BareRoot()

	_, err := p.Provision(store, params)
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
		Name:  "test",
		Owner: "user",
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
		Name:  "test",
		Owner: "user",
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
	tw := setupTestWorkspace(t, "github.com", "test/myrepo")
	upstream := createUpstreamRepo(t, tw.WorkspaceRoot)

	actLogPath := filepath.Join(tw.WorkspaceRoot, "activity.log")
	actLog := NewActivityLog(actLogPath)

	store := newMemStore()
	p := &Provisioner{
		ActivityLog: actLog,
	}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "test",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstream},
			Owner: currentUser(),
		},
		WorkspaceRoot: tw.WorkspaceRoot,
	}

	inst, err := p.Provision(store, params)
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

func TestProvision_ActivityLogNoIDEEvents(t *testing.T) {
	tw := setupTestWorkspace(t, "github.com", "test/myrepo")
	upstream := createUpstreamRepo(t, tw.WorkspaceRoot)

	actLogPath := filepath.Join(tw.WorkspaceRoot, "activity.log")
	actLog := NewActivityLog(actLogPath)

	portFile := filepath.Join(tw.WorkspaceRoot, "ports.json")
	store := newMemStore()
	p := &Provisioner{
		ActivityLog:   actLog,
		IDEAdapter:    &stubIDEAdapter{},
		PortAllocator: NewPortAllocator(portFile),
	}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "test",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstream},
			Owner: currentUser(),
		},
		WorkspaceRoot: tw.WorkspaceRoot,
	}

	_, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// IDE startup is deferred — no IDE events should be in the activity log
	ideRecords, err := actLog.Read(ActivityLogFilter{ScopeKind: ScopeKindIDE})
	if err != nil {
		t.Fatal(err)
	}
	if len(ideRecords) != 0 {
		t.Fatalf("expected 0 IDE activity log records (IDE deferred), got %d", len(ideRecords))
	}

	// Workspace events should still be present
	wsRecords, err := actLog.Read(ActivityLogFilter{ScopeKind: ScopeKindWorkspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(wsRecords) == 0 {
		t.Fatal("expected workspace events in activity log")
	}
}

func TestProvision_NilActivityLogDoesNotPanic(t *testing.T) {
	tw := setupTestWorkspace(t, "github.com", "test/myrepo")
	upstream := createUpstreamRepo(t, tw.WorkspaceRoot)

	store := newMemStore()
	p := &Provisioner{
		// ActivityLog intentionally nil
	}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "test",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstream},
			Owner: currentUser(),
		},
		WorkspaceRoot: tw.WorkspaceRoot,
	}

	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}
}

func TestProvision_EventRecordScopesCorrectOnIdempotent(t *testing.T) {
	tw := setupTestWorkspace(t, "github.com", "org/repo")

	// Simulate existing worktree with .git directory
	os.MkdirAll(filepath.Join(tw.DefaultRoot, ".git"), 0755)

	actLogPath := filepath.Join(tw.WorkspaceRoot, "activity.log")
	actLog := NewActivityLog(actLogPath)

	store := newMemStore()
	p := &Provisioner{
		ActivityLog: actLog,
	}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myws",
			VCS:   VCSTarget{Host: "github.com", Repo: "org/repo", CloneURL: "https://github.com/org/repo.git"},
			Owner: "testuser",
		},
		WorkspaceRoot: tw.WorkspaceRoot,
	}

	inst, err := p.Provision(store, params)
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

// --- AddWorktree tests ---

func TestAddWorktree_NotFound(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{}

	_, err := p.AddWorktree(store, "nonexistent", "feat/x")
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

func TestAddWorktree_IdempotentFromState(t *testing.T) {
	store := newMemStore()
	store.instances["myws"] = &Workspace{
		RepoRoot: "/tmp/fake/repo",
		BareRoot: "/tmp/fake/repo/.bare",
		Owner:    "testuser",
		Worktrees: []Worktree{
			{Name: "feat/bar", Branch: "feat/bar", ProjectRoot: "/tmp/fake/repo/.worktrees/feat/bar", IsDefault: false},
		},
	}
	p := &Provisioner{}

	result, err := p.AddWorktree(store, "myws", "feat/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Created {
		t.Fatal("expected Created=false for idempotent return")
	}
	if result.ProjectRoot != "/tmp/fake/repo/.worktrees/feat/bar" {
		t.Fatalf("unexpected project root: %s", result.ProjectRoot)
	}
	if result.WorkspaceName != "myws" {
		t.Fatalf("unexpected workspace name: %s", result.WorkspaceName)
	}
}

func TestAddWorktree_RealGit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "feat/new-feature", "feature.txt", "# feature\n")

	store := newMemStore()
	p := &Provisioner{}

	// First provision the workspace (bare clone + default worktree)
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:    "myrepo",
			VCS:     VCSTarget{Host: "github.com", Repo: "org/myrepo", CloneURL: upstreamBare},
			PatName: "gh-token",
			Owner:   currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	ws, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}
	if ws.Status != StatusReady {
		t.Fatalf("expected ready, got %s", ws.Status)
	}

	// Now add a worktree for the feature branch
	result, err := p.AddWorktree(store, "myrepo", "feat/new-feature")
	if err != nil {
		t.Fatalf("AddWorktree failed: %v", err)
	}
	if !result.Created {
		t.Fatal("expected Created=true for new worktree")
	}
	expectedRoot := DeriveProjectRoot(ws.RepoRoot, "feat/new-feature")
	if result.ProjectRoot != expectedRoot {
		t.Fatalf("expected project root %s, got %s", expectedRoot, result.ProjectRoot)
	}

	// Verify the worktree directory exists on disk
	if !worktreeExists(result.ProjectRoot) {
		t.Fatal("worktree directory does not exist on disk")
	}

	// Verify the feature file exists in the worktree
	featureFile := filepath.Join(result.ProjectRoot, "feature.txt")
	if _, err := os.Stat(featureFile); err != nil {
		t.Fatalf("feature.txt not found in worktree: %v", err)
	}

	// Verify worktree was appended to aggregate state
	updated := store.instances["myrepo"]
	found := false
	for _, wt := range updated.Worktrees {
		if wt.Branch == "feat/new-feature" {
			found = true
			if wt.IsDefault {
				t.Fatal("non-default worktree should have IsDefault=false")
			}
			if wt.ProjectRoot != expectedRoot {
				t.Fatalf("worktree project root mismatch: %s", wt.ProjectRoot)
			}
		}
	}
	if !found {
		t.Fatal("worktree not found in aggregate state")
	}

	// Verify events were emitted
	hasCreating := false
	hasCreated := false
	for _, ev := range updated.Events {
		if ev.Event == string(EventWorktreeCreating) && strings.Contains(ev.Detail, "feat/new-feature") {
			hasCreating = true
		}
		if ev.Event == string(EventWorktreeCreated) && strings.Contains(ev.Detail, "feat/new-feature") {
			hasCreated = true
		}
	}
	if !hasCreating {
		t.Fatal("missing worktree_creating event")
	}
	if !hasCreated {
		t.Fatal("missing worktree_created event")
	}
}

func TestAddWorktree_IdempotentSecondCall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepo(t, dir)
	addUpstreamBranch(t, dir, "bugfix", "fix.txt", "# fix\n")

	store := newMemStore()
	p := &Provisioner{}

	// Provision workspace
	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:    "myrepo",
			VCS:     VCSTarget{Host: "github.com", Repo: "org/myrepo", CloneURL: upstreamBare},
			PatName: "gh-token",
			Owner:   currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}
	_, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// First add
	result1, err := p.AddWorktree(store, "myrepo", "bugfix")
	if err != nil {
		t.Fatalf("first AddWorktree failed: %v", err)
	}
	if !result1.Created {
		t.Fatal("first call should return Created=true")
	}

	// Second add (idempotent — found in state)
	result2, err := p.AddWorktree(store, "myrepo", "bugfix")
	if err != nil {
		t.Fatalf("second AddWorktree failed: %v", err)
	}
	if result2.Created {
		t.Fatal("second call should return Created=false")
	}
	if result2.ProjectRoot != result1.ProjectRoot {
		t.Fatalf("project root mismatch: %s vs %s", result1.ProjectRoot, result2.ProjectRoot)
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

// --- Submodule tests ---

// createUpstreamRepoWithSubmodule creates an upstream repo that includes a
// submodule. Returns the upstream bare path. The submodule repo is a separate
// bare repo at <dir>/sub-upstream.git. The submodule is added at path "libs/sub".
func createUpstreamRepoWithSubmodule(t *testing.T, dir string) string {
	t.Helper()

	// Create the submodule upstream
	subUpstream := filepath.Join(dir, "sub-upstream.git")
	runGit(t, "", "init", "--bare", subUpstream)
	subScratch := filepath.Join(dir, "sub-scratch")
	runGit(t, "", "clone", subUpstream, subScratch)
	os.WriteFile(filepath.Join(subScratch, "lib.go"), []byte("package lib\n"), 0644)
	runGit(t, subScratch, "add", ".")
	runGit(t, subScratch, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "init sub")
	runGit(t, subScratch, "push", "origin", "main")

	// Create the main upstream with a submodule reference
	mainUpstream := filepath.Join(dir, "upstream.git")
	runGit(t, "", "init", "--bare", mainUpstream)
	mainScratch := filepath.Join(dir, "scratch")
	runGit(t, "", "clone", mainUpstream, mainScratch)
	os.WriteFile(filepath.Join(mainScratch, "README.md"), []byte("# main\n"), 0644)
	runGit(t, mainScratch, "add", ".")
	runGit(t, mainScratch, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "init")

	// Add submodule
	runGit(t, mainScratch, "-c", "protocol.file.allow=always", "submodule", "add", subUpstream, "libs/sub")
	runGit(t, mainScratch, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "add submodule")
	runGit(t, mainScratch, "push", "origin", "main")

	return mainUpstream
}

func TestSubmoduleUpdate_CommandConstruction(t *testing.T) {
	// Unit test: verify submoduleUpdate and submoduleSync return errors for
	// non-existent paths (no git repo). This validates the command is constructed
	// and executed.
	p := &Provisioner{}

	// Non-existent path should fail
	err := p.submoduleUpdate("/nonexistent/path", "")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}

	err = p.submoduleSync("/nonexistent/path", "")
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}

func TestSubmoduleUpdate_NoSubmodules(t *testing.T) {
	// submodule update --init --recursive on a repo without submodules is a no-op
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	// Create a simple repo with no submodules
	repoDir := filepath.Join(dir, "repo")
	runGit(t, "", "init", repoDir)
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# test\n"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "init")

	p := &Provisioner{}
	// Should succeed (no-op)
	if err := p.submoduleUpdate(repoDir, ""); err != nil {
		t.Fatalf("submoduleUpdate on repo without submodules should be no-op, got: %v", err)
	}
	if err := p.submoduleSync(repoDir, ""); err != nil {
		t.Fatalf("submoduleSync on repo without submodules should be no-op, got: %v", err)
	}
}

func TestProvisionBareCloneAndDefault_WithSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepoWithSubmodule(t, dir)

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	// Override protocol.file.allow for test (submodule uses file:// URL)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")

	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}
	if inst.Status != StatusReady {
		t.Fatalf("expected ready, got %s", inst.Status)
	}

	// Verify submodule directory is populated
	projectRoot := params.ProjectRoot()
	submodulePath := filepath.Join(projectRoot, "libs", "sub", "lib.go")
	if _, err := os.Stat(submodulePath); os.IsNotExist(err) {
		t.Fatalf("submodule file libs/sub/lib.go was not populated after provision")
	}

	// Verify submodule events were emitted
	hasSubmoduleStart := false
	hasSubmoduleComplete := false
	for _, ev := range inst.Events {
		if ev.Event == string(EventSubmoduleInitStarted) {
			hasSubmoduleStart = true
		}
		if ev.Event == string(EventSubmoduleInitCompleted) {
			hasSubmoduleComplete = true
		}
	}
	if !hasSubmoduleStart {
		t.Fatal("expected submodule_init_started event")
	}
	if !hasSubmoduleComplete {
		t.Fatal("expected submodule_init_completed event")
	}
}

func TestHydrate_UpdatesSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepoWithSubmodule(t, dir)

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")

	// Step 1: Provision
	inst, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// Step 2: Update the submodule in the upstream (add a new file to sub-upstream)
	subUpstream := filepath.Join(dir, "sub-upstream.git")
	subScratch := filepath.Join(dir, "sub-scratch")
	os.WriteFile(filepath.Join(subScratch, "new_file.go"), []byte("package lib // new\n"), 0644)
	runGit(t, subScratch, "add", ".")
	runGit(t, subScratch, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "add new_file")
	runGit(t, subScratch, "push", "origin", "main")

	// Step 3: Update the submodule reference in the main repo's upstream
	mainScratch := filepath.Join(dir, "scratch")
	runGit(t, filepath.Join(mainScratch, "libs", "sub"), "pull", "origin", "main")
	runGit(t, mainScratch, "add", "libs/sub")
	runGit(t, mainScratch, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "update submodule ref")
	runGit(t, mainScratch, "push", "origin", "main")

	// Step 4: Verify the new file doesn't exist yet in the workspace worktree
	projectRoot := params.ProjectRoot()
	newFilePath := filepath.Join(projectRoot, "libs", "sub", "new_file.go")
	if _, err := os.Stat(newFilePath); err == nil {
		t.Fatal("new_file.go should not exist before hydration")
	}

	// Step 5: Re-provision (idempotent path triggers hydration)
	eventCountBefore := len(inst.Events)
	inst2, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("re-provision failed: %v", err)
	}
	_ = subUpstream // keep reference

	// Step 6: Verify the submodule was updated (new file should now exist)
	if _, err := os.Stat(newFilePath); os.IsNotExist(err) {
		t.Fatal("new_file.go should exist after hydration pulled updated submodule ref")
	}

	// Verify that submodule events were emitted during hydration
	hasSubmoduleEvent := false
	for i := eventCountBefore; i < len(inst2.Events); i++ {
		if inst2.Events[i].Event == string(EventSubmoduleInitStarted) ||
			inst2.Events[i].Event == string(EventSubmoduleInitCompleted) {
			hasSubmoduleEvent = true
			break
		}
	}
	if !hasSubmoduleEvent {
		t.Fatal("expected submodule events during hydration")
	}
}

func TestAddWorktree_InitializesSubmodules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	upstreamBare := createUpstreamRepoWithSubmodule(t, dir)

	// Also create a feature branch on upstream
	mainScratch := filepath.Join(dir, "scratch")
	runGit(t, mainScratch, "checkout", "-b", "feature-x")
	os.WriteFile(filepath.Join(mainScratch, "feature.go"), []byte("package main\n"), 0644)
	runGit(t, mainScratch, "add", ".")
	runGit(t, mainScratch, "-c", "user.name=Test", "-c", "user.email=t@t.com", "commit", "-m", "feature branch")
	runGit(t, mainScratch, "push", "origin", "feature-x")

	store := newMemStore()
	p := &Provisioner{}

	params := ProvisionParams{
		Spec: WorkspaceSpec{
			Name:  "myrepo",
			VCS:   VCSTarget{Host: "github.com", Repo: "test/myrepo", CloneURL: upstreamBare},
			Owner: currentUser(),
		},
		WorkspaceRoot: filepath.Join(dir, "code"),
	}

	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "protocol.file.allow")
	t.Setenv("GIT_CONFIG_VALUE_0", "always")

	// Step 1: Provision default worktree
	_, err := p.Provision(store, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// Step 2: Add a worktree for the feature branch
	result, err := p.AddWorktree(store, "myrepo", "feature-x")
	if err != nil {
		t.Fatalf("add worktree failed: %v", err)
	}
	if !result.Created {
		t.Fatal("expected worktree to be created")
	}

	// Step 3: Verify submodule is populated in the new worktree
	submodulePath := filepath.Join(result.ProjectRoot, "libs", "sub", "lib.go")
	if _, err := os.Stat(submodulePath); os.IsNotExist(err) {
		t.Fatal("submodule file libs/sub/lib.go was not populated in new worktree")
	}
}

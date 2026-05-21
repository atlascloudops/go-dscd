package domain

import (
	"path/filepath"
	"testing"
)

func TestParseWorktreeListPorcelain_Basic(t *testing.T) {
	bareRoot := "/home/jperez/code/github.com/atlasops/infra/.bare"
	output := `worktree /home/jperez/code/github.com/atlasops/infra/.bare
HEAD abc123
branch refs/heads/main
bare

worktree /home/jperez/code/github.com/atlasops/infra/default
HEAD abc123
branch refs/heads/main

worktree /home/jperez/code/github.com/atlasops/infra/.worktrees/feature-vpc
HEAD def456
branch refs/heads/feature-vpc

worktree /home/jperez/code/github.com/atlasops/infra/.worktrees/bugfix-42
HEAD 789aaa
branch refs/heads/bugfix-42

`

	names := parseWorktreeListPorcelain(output, bareRoot)
	if len(names) != 3 {
		t.Fatalf("expected 3 worktrees, got %d: %v", len(names), names)
	}
	expected := []string{"default", "feature-vpc", "bugfix-42"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestParseWorktreeListPorcelain_SkipsBareRoot(t *testing.T) {
	bareRoot := "/tmp/repo/.bare"
	output := `worktree /tmp/repo/.bare
HEAD abc123
branch refs/heads/main
bare

worktree /tmp/repo/default
HEAD abc123
branch refs/heads/main

`

	names := parseWorktreeListPorcelain(output, bareRoot)
	if len(names) != 1 {
		t.Fatalf("expected 1 worktree (bare root excluded), got %d: %v", len(names), names)
	}
	if names[0] != "default" {
		t.Errorf("expected 'default', got %q", names[0])
	}
}

func TestParseWorktreeListPorcelain_Empty(t *testing.T) {
	names := parseWorktreeListPorcelain("", "/tmp/.bare")
	if len(names) != 0 {
		t.Fatalf("expected 0 worktrees for empty output, got %d", len(names))
	}
}

func TestParseWorktreeListPorcelain_OnlyBareRoot(t *testing.T) {
	bareRoot := "/tmp/repo/.bare"
	output := `worktree /tmp/repo/.bare
HEAD abc123
branch refs/heads/main
bare

`
	names := parseWorktreeListPorcelain(output, bareRoot)
	if len(names) != 0 {
		t.Fatalf("expected 0 worktrees (only bare root), got %d: %v", len(names), names)
	}
}

func TestParseWorktreeListPorcelain_TrailingSlashNormalization(t *testing.T) {
	// Bare root with trailing slash should still be excluded
	bareRoot := "/tmp/repo/.bare/"
	output := `worktree /tmp/repo/.bare
HEAD abc123
bare

worktree /tmp/repo/default
HEAD abc123
branch refs/heads/main

`
	names := parseWorktreeListPorcelain(output, bareRoot)
	if len(names) != 1 {
		t.Fatalf("expected 1 worktree, got %d: %v", len(names), names)
	}
	if names[0] != "default" {
		t.Errorf("expected 'default', got %q", names[0])
	}
}

func TestListWorktrees_RealGit(t *testing.T) {
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

	// Provision default (bare clone + default worktree)
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
	if _, err := p.Provision(store, defaultSpec); err != nil {
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
	if _, err := p.Provision(store, featureSpec); err != nil {
		t.Fatalf("feature provision failed: %v", err)
	}

	// AC: ListWorktrees returns both worktree names, excluding the bare root
	names, err := ListWorktrees(bareRoot, currentUser())
	if err != nil {
		t.Fatalf("ListWorktrees failed: %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 worktrees, got %d: %v", len(names), names)
	}

	// Verify both expected names appear (order may vary)
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["default"] {
		t.Error("expected 'default' in worktree list")
	}
	if !found["feature-vpc"] {
		t.Error("expected 'feature-vpc' in worktree list")
	}
}

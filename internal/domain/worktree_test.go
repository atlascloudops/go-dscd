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

func TestParseWorktreeEntries_Basic(t *testing.T) {
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

`

	entries := parseWorktreeEntries(output, bareRoot)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Path != "/home/jperez/code/github.com/atlasops/infra/default" {
		t.Errorf("entries[0].Path = %q", entries[0].Path)
	}
	if entries[0].Branch != "main" {
		t.Errorf("entries[0].Branch = %q, want main", entries[0].Branch)
	}
	if entries[1].Path != "/home/jperez/code/github.com/atlasops/infra/.worktrees/feature-vpc" {
		t.Errorf("entries[1].Path = %q", entries[1].Path)
	}
	if entries[1].Branch != "feature-vpc" {
		t.Errorf("entries[1].Branch = %q, want feature-vpc", entries[1].Branch)
	}
}

func TestParseWorktreeEntries_Empty(t *testing.T) {
	entries := parseWorktreeEntries("", "/tmp/.bare")
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries for empty output, got %d", len(entries))
	}
}

func TestParseWorktreeEntries_OnlyBare(t *testing.T) {
	output := `worktree /tmp/repo/.bare
HEAD abc123
bare

`
	entries := parseWorktreeEntries(output, "/tmp/repo/.bare")
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
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
	p := &Provisioner{}

	// Provision default (bare clone + default worktree)
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
	if _, err := p.Provision(store, defaultParams); err != nil {
		t.Fatalf("default provision failed: %v", err)
	}

	// Provision feature worktree
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
	if _, err := p.Provision(store, featureParams); err != nil {
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

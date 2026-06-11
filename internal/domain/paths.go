package domain

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultWorkspaceRoot = "~/code"

// ResolveWorkspaceRoot returns the workspace root directory.
// It checks the DSCD_WORKSPACE_ROOT environment variable first,
// falling back to ~/code expanded using the provided owner.
func ResolveWorkspaceRoot(owner string) string {
	if envRoot := os.Getenv("DSCD_WORKSPACE_ROOT"); envRoot != "" {
		return envRoot
	}
	return expandHome(defaultWorkspaceRoot, owner)
}

// DeriveRepoRoot returns the repo container directory for a VCS workspace.
// Convention: <workspace_root>/<host>/<slug>/
func DeriveRepoRoot(workspaceRoot, host, slug string) string {
	return filepath.Join(workspaceRoot, host, slug)
}

// DeriveBareRoot returns the bare clone directory within a repo root.
// Convention: <repo_root>/.bare
func DeriveBareRoot(repoRoot string) string {
	return filepath.Join(repoRoot, ".bare")
}

// DeriveProjectRoot returns the worktree checkout directory.
// Convention:
//   - default worktree: <repo_root>/default
//   - named worktree:   <repo_root>/.worktrees/<branch>
func DeriveProjectRoot(repoRoot, worktreeName string) string {
	if worktreeName == "default" {
		return filepath.Join(repoRoot, "default")
	}
	return filepath.Join(repoRoot, ".worktrees", worktreeName)
}

// DeriveLocalRepoRoot returns the repo container directory for a
// template-only workspace (no remote VCS).
// Convention: <workspace_root>/local/<name>
func DeriveLocalRepoRoot(workspaceRoot, name string) string {
	return filepath.Join(workspaceRoot, "local", name)
}

// expandHome replaces a leading ~ with /home/<owner>.
func expandHome(path, owner string) string {
	if len(path) == 0 {
		return path
	}
	if path[0] == '~' {
		return fmt.Sprintf("/home/%s%s", owner, path[1:])
	}
	return path
}

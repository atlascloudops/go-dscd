package domain

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsWorktreeDirty returns true if the worktree at projectRoot has uncommitted
// or untracked changes (non-empty output from git status --porcelain).
// Commands run as owner when it differs from the current user.
func IsWorktreeDirty(projectRoot, owner string) (bool, error) {
	statusCmd := fmt.Sprintf("git -C %s status --porcelain", projectRoot)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", statusCmd)
	} else {
		cmd = exec.Command("git", "-C", projectRoot, "status", "--porcelain")
	}

	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status failed: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// ListWorktrees enumerates worktree directory names from a bare clone using
// git worktree list --porcelain. It runs as the given owner (su) when dscd
// is running as root inspecting a user-owned workspace.
// Returns the basename of each worktree path, e.g. ["default", "feature-vpc"].
func ListWorktrees(bareRoot, owner string) ([]string, error) {
	gitCmd := fmt.Sprintf("git -C %s worktree list --porcelain", bareRoot)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", gitCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "worktree", "list", "--porcelain")
	}

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w", err)
	}

	return parseWorktreeListPorcelain(string(out), bareRoot), nil
}

// parseWorktreeListPorcelain extracts worktree directory names from the
// porcelain output of git worktree list. The format emits blocks separated
// by blank lines; each block starts with "worktree <path>".
// The bare root entry (marked with a "bare" line) is excluded.
func parseWorktreeListPorcelain(output, bareRoot string) []string {
	// Split into blocks separated by blank lines.
	// Each block has: "worktree <path>\nHEAD <sha>\nbranch <ref>\n" or "bare" instead of branch.
	blocks := strings.Split(strings.TrimSpace(output), "\n\n")

	var names []string
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		lines := strings.Split(block, "\n")
		if len(lines) == 0 {
			continue
		}

		// First line must be "worktree <path>"
		if !strings.HasPrefix(lines[0], "worktree ") {
			continue
		}
		wtPath := strings.TrimPrefix(lines[0], "worktree ")

		// Skip the bare root entry — identified by the "bare" marker line
		isBare := false
		for _, l := range lines[1:] {
			if strings.TrimSpace(l) == "bare" {
				isBare = true
				break
			}
		}
		if isBare {
			continue
		}

		names = append(names, filepath.Base(wtPath))
	}
	return names
}

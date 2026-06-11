package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceWorktreeCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	worktree := &cobra.Command{
		Use:   "worktree",
		Short: "Manage worktrees within a workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	worktree.AddCommand(newWorktreeAddCmd(store, activityLog))
	return worktree
}

func newWorktreeAddCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	return &cobra.Command{
		Use:   "add <workspace> <branch>",
		Short: "Create a worktree for a branch in an existing workspace",
		Long: `Create a worktree for a specific branch within an existing workspace's bare clone.

The branch is fetched from origin and a worktree is created at
<repo_root>/.worktrees/<branch>. The operation is idempotent — if the
worktree already exists, the existing project root is returned.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceName := args[0]
			branch := args[1]

			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			result, err := provisioner.AddWorktree(store, workspaceName, branch)
			if err != nil {
				if pe, ok := err.(*domain.ProvisionError); ok {
					resp := domain.ErrorResponse("workspace.worktree.add", domain.ErrorInfo{
						Code:    pe.Code,
						Message: pe.Message,
						Detail:  pe.Detail,
					})
					return outputResponse(resp, 1)
				}
				resp := domain.ErrorResponse("workspace.worktree.add", domain.ErrorInfo{
					Code:    "INTERNAL",
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			if !jsonOutput {
				if result.Created {
					fmt.Printf("Worktree '%s' created at %s\n", result.Branch, result.ProjectRoot)
				} else {
					fmt.Printf("Worktree '%s' already exists at %s\n", result.Branch, result.ProjectRoot)
				}
				return nil
			}

			resp := domain.OkResponse("workspace.worktree.add", result)
			return outputResponse(resp, 0)
		},
	}
}

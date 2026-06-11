package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceDeprovisionCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	var force bool
	var worktree string
	var all bool // backward-compat: accepted but ignored (remove-all is the default)

	cmd := &cobra.Command{
		Use:   "deprovision <name>",
		Short: "Remove a workspace or a single worktree",
		Long: `Remove a workspace or a single worktree.

By default, removes the entire workspace: stops all IDE instances, removes
all worktrees, removes the bare clone and repo container, and deletes the
state entry.

With --worktree <branch>, removes only the specified worktree from the
workspace aggregate. The default worktree cannot be removed this way —
deprovision the entire workspace instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			var result *domain.DeprovisionResult
			var err error

			if worktree != "" {
				result, err = provisioner.DeprovisionWorktree(store, name, worktree, force)
			} else {
				result, err = provisioner.Deprovision(store, name, force)
			}

			if err != nil {
				if pe, ok := err.(*domain.ProvisionError); ok {
					resp := domain.ErrorResponse("workspace.deprovision", domain.ErrorInfo{
						Code:    pe.Code,
						Message: pe.Message,
						Detail:  pe.Detail,
					})
					return outputResponse(resp, 1)
				}
				resp := domain.ErrorResponse("workspace.deprovision", domain.ErrorInfo{
					Code:    "INTERNAL",
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			resp := domain.OkResponse("workspace.deprovision", result)

			if !jsonOutput {
				fmt.Println(result.Message)
				return nil
			}

			return outputResponse(resp, 0)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Delete even if worktrees have uncommitted changes")
	cmd.Flags().StringVar(&worktree, "worktree", "", "Remove only the specified worktree (by branch name)")
	cmd.Flags().BoolVar(&all, "all", false, "Remove entire workspace (default behavior, kept for backward compatibility)")
	cmd.Flags().MarkHidden("all")

	return cmd
}

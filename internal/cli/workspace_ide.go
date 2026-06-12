package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceIDECmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	ide := &cobra.Command{
		Use:   "ide",
		Short: "Manage IDE instances within a workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	ide.AddCommand(
		newIDEStartCmd(store, activityLog),
		newIDEStopCmd(store, activityLog),
	)
	return ide
}

func newIDEStartCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	var worktreeName string

	cmd := &cobra.Command{
		Use:   "start <workspace>",
		Short: "Start an IDE instance for a worktree",
		Long: `Start an IDE instance (openvscode-server) for a specific worktree within a workspace.

By default, starts the IDE for the 'default' worktree. Use --worktree to target
a different worktree. The operation is idempotent — if an IDE is already running,
its current state is returned.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceName := args[0]

			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			result, err := provisioner.StartIDE(store, workspaceName, worktreeName)
			if err != nil {
				if pe, ok := err.(*domain.ProvisionError); ok {
					resp := domain.ErrorResponse("workspace.ide.start", domain.ErrorInfo{
						Code:    pe.Code,
						Message: pe.Message,
						Detail:  pe.Detail,
					})
					return outputResponse(resp, 1)
				}
				resp := domain.ErrorResponse("workspace.ide.start", domain.ErrorInfo{
					Code:    "INTERNAL",
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			if !jsonOutput {
				fmt.Printf("IDE started for worktree '%s' in workspace '%s' (port %d, status: %s)\n",
					result.WorktreeName, result.WorkspaceName, result.Port, result.Status)
				return nil
			}

			resp := domain.OkResponse("workspace.ide.start", result)
			return outputResponse(resp, 0)
		},
	}

	cmd.Flags().StringVar(&worktreeName, "worktree", "", "worktree name (default: 'default')")
	return cmd
}

func newIDEStopCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	var worktreeName string

	cmd := &cobra.Command{
		Use:   "stop <workspace>",
		Short: "Stop an IDE instance for a worktree",
		Long: `Stop a running IDE instance for a specific worktree within a workspace.

By default, stops the IDE for the 'default' worktree. Use --worktree to target
a different worktree.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaceName := args[0]

			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			result, err := provisioner.StopIDE(store, workspaceName, worktreeName)
			if err != nil {
				if pe, ok := err.(*domain.ProvisionError); ok {
					resp := domain.ErrorResponse("workspace.ide.stop", domain.ErrorInfo{
						Code:    pe.Code,
						Message: pe.Message,
						Detail:  pe.Detail,
					})
					return outputResponse(resp, 1)
				}
				resp := domain.ErrorResponse("workspace.ide.stop", domain.ErrorInfo{
					Code:    "INTERNAL",
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			if !jsonOutput {
				fmt.Println(result.Message)
				return nil
			}

			resp := domain.OkResponse("workspace.ide.stop", result)
			return outputResponse(resp, 0)
		},
	}

	cmd.Flags().StringVar(&worktreeName, "worktree", "", "worktree name (default: 'default')")
	return cmd
}

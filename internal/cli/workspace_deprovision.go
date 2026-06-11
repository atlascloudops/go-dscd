package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceDeprovisionCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "deprovision <name>",
		Short: "Remove a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			result, err := provisioner.Deprovision(store, name, force)
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

	return cmd
}

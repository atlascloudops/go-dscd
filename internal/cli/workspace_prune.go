package cli

import (
	"fmt"
	"strings"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspacePruneCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune <workspace>",
		Short: "Remove all clean non-default worktrees for a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoName := args[0]
			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			result, err := provisioner.Prune(store, repoName)
			if err != nil {
				if pe, ok := err.(*domain.ProvisionError); ok {
					resp := domain.ErrorResponse("workspace.prune", domain.ErrorInfo{
						Code:    pe.Code,
						Message: pe.Message,
						Detail:  pe.Detail,
					})
					return outputResponse(resp, 1)
				}
				resp := domain.ErrorResponse("workspace.prune", domain.ErrorInfo{
					Code:    "INTERNAL",
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			resp := domain.OkResponse("workspace.prune", result)

			if !jsonOutput {
				if len(result.Pruned) == 0 && len(result.Skipped) == 0 {
					fmt.Println(result.Message)
					return nil
				}

				if len(result.Pruned) > 0 {
					fmt.Printf("Pruned:   %s\n", strings.Join(result.Pruned, ", "))
				}

				if len(result.Skipped) > 0 {
					var skipParts []string
					for _, s := range result.Skipped {
						skipParts = append(skipParts, fmt.Sprintf("%s (%s)", s.Name, s.Reason))
					}
					fmt.Printf("Skipped:  %s\n", strings.Join(skipParts, ", "))
				}

				fmt.Println(result.Message)
				return nil
			}

			return outputResponse(resp, 0)
		},
	}

	return cmd
}

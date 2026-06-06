package cli

import (
	"fmt"
	"strings"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspacePruneCmd(store domain.StateStore, logDir string, activityLog *domain.ActivityLog) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune <workspace>",
		Short: "Remove all clean non-default worktrees for a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoName := args[0]
			provisioner := &domain.Provisioner{
				LogDir:        logDir,
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

			// Human-readable output when not JSON
			if !jsonOutput {
				if len(result.Pruned) == 0 && len(result.Skipped) == 0 {
					fmt.Println(result.Message)
					return nil
				}

				if len(result.Pruned) > 0 {
					// Extract worktree names from full workspace names
					var wtNames []string
					for _, name := range result.Pruned {
						parts := strings.SplitN(name, "/", 2)
						if len(parts) == 2 {
							wtNames = append(wtNames, parts[1])
						} else {
							wtNames = append(wtNames, name)
						}
					}
					fmt.Printf("Pruned:   %s\n", strings.Join(wtNames, ", "))
				}

				if len(result.Skipped) > 0 {
					var skipParts []string
					for _, s := range result.Skipped {
						parts := strings.SplitN(s.Name, "/", 2)
						wtName := s.Name
						if len(parts) == 2 {
							wtName = parts[1]
						}
						skipParts = append(skipParts, fmt.Sprintf("%s (%s)", wtName, s.Reason))
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

package cli

import (
	"fmt"
	"strings"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceDeprovisionCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	var force bool
	var all bool

	cmd := &cobra.Command{
		Use:   "deprovision <name>",
		Short: "Remove a workspace worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			var result *domain.DeprovisionResult
			var err error

			if all {
				result, err = provisioner.DeprovisionAll(store, name, force)
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

			// Human-readable output when not JSON
			if !jsonOutput {
				if all {
					var wtNames []string
					for _, r := range result.Removed {
						parts := strings.SplitN(r, "/", 2)
						if len(parts) == 2 {
							wtNames = append(wtNames, parts[1])
						} else {
							wtNames = append(wtNames, "default")
						}
					}
					fmt.Printf("Removed worktrees: %s\n", strings.Join(wtNames, ", "))
					fmt.Println(result.Message)
				} else {
					if force {
						fmt.Println(result.Message)
					} else {
						fmt.Println("No uncommitted changes. Removing worktree.")
						fmt.Println(result.Message)
					}
				}
				return nil
			}

			return outputResponse(resp, 0)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Delete even if worktree has uncommitted changes")
	cmd.Flags().BoolVar(&all, "all", false, "Remove all worktrees and the bare clone for this workspace")

	return cmd
}

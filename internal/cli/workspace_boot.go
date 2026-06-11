package cli

import (
	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceBootCmd(store domain.StateStore, al *domain.ActivityLog, workspaceRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "boot",
		Short: "Hydrate and sync workspaces (systemd entry point)",
		Long:  "Composes hydrate (discover unknown workspaces from disk) then sync (health-check all known workspaces). Intended as the systemd ExecStart command.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve workspace root: flag > env > default
			wsRoot := *workspaceRoot
			if wsRoot == "" {
				wsRoot = domain.ResolveWorkspaceRoot("")
			}

			syncer := domain.NewSyncer(store, al).WithIDE(
				domain.NewCodeServerAdapter(),
				domain.NewPortAllocator(defaultPortFile),
			)
			report, err := syncer.Boot(wsRoot)
			if err != nil {
				resp := domain.ErrorResponse("workspace.boot", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}
			resp := domain.OkResponse("workspace.boot", report)
			return outputResponse(resp, 0)
		},
	}
}

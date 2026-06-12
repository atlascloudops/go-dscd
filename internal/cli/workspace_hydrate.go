package cli

import (
	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceHydrateCmd(store domain.StateStore, al *domain.ActivityLog, workspaceRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "hydrate",
		Short: "Discover workspaces from disk and add to state",
		Long:  "Scans the workspace root for bare clones not yet tracked in state and reconstructs Workspace entries from disk.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve workspace root: flag > env > default
			wsRoot := *workspaceRoot
			if wsRoot == "" {
				wsRoot = domain.ResolveWorkspaceRoot("")
			}

			syncer := domain.NewSyncer(store, al)
			report, err := syncer.Hydrate(wsRoot)
			if err != nil {
				resp := domain.ErrorResponse("workspace.hydrate", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}
			resp := domain.OkResponse("workspace.hydrate", report)
			return outputResponse(resp, 0)
		},
	}
}

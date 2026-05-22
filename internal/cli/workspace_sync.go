package cli

import (
	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceSyncCmd(store domain.StateStore, logDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync workspace state with filesystem reality",
		RunE: func(cmd *cobra.Command, args []string) error {
			syncer := domain.NewSyncer(store, logDir).WithIDE(
				domain.NewCodeServerAdapter(),
				domain.NewPortAllocator(defaultPortFile),
			)
			report, err := syncer.Sync()
			if err != nil {
				resp := domain.ErrorResponse("workspace.sync", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}
			resp := domain.OkResponse("workspace.sync", report)
			return outputResponse(resp, 0)
		},
	}
}

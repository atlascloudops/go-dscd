package cli

import (
	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceReconcileCmd(store domain.StateStore, logDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile workspace state with filesystem",
		RunE: func(cmd *cobra.Command, args []string) error {
			reconciler := domain.NewReconciler(store, logDir)
			report, err := reconciler.Reconcile()
			if err != nil {
				resp := domain.ErrorResponse("workspace.reconcile", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}
			resp := domain.OkResponse("workspace.reconcile", report)
			return outputResponse(resp, 0)
		},
	}
}

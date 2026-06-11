package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceProvisionCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	return &cobra.Command{
		Use:   "provision <params-json>",
		Short: "Provision a workspace from a JSON params object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var params domain.ProvisionParams
			if err := json.Unmarshal([]byte(args[0]), &params); err != nil {
				resp := domain.ErrorResponse("workspace.provision", domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "invalid JSON params",
					Detail:  err.Error(),
				})
				return outputResponse(resp, 1)
			}

			provisioner := &domain.Provisioner{
				IDEAdapter:    domain.NewCodeServerAdapter(),
				PortAllocator: domain.NewPortAllocator(defaultPortFile),
				ActivityLog:   activityLog,
			}

			ws, err := provisioner.Provision(store, params)
			if err != nil {
				if pe, ok := err.(*domain.ProvisionError); ok {
					resp := domain.ErrorResponse("workspace.provision", domain.ErrorInfo{
						Code:    pe.Code,
						Message: pe.Message,
						Detail:  pe.Detail,
					})
					return outputResponse(resp, 1)
				}
				resp := domain.ErrorResponse("workspace.provision", domain.ErrorInfo{
					Code:    "INTERNAL",
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			resp := domain.OkResponse("workspace.provision", ws)
			return outputResponse(resp, 0)
		},
	}
}

func outputResponse(resp domain.Response, exitCode int) error {
	if jsonOutput {
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	} else {
		if resp.Status == "error" && resp.Error != nil {
			fmt.Fprintf(os.Stderr, "Error [%s]: %s\n", resp.Error.Code, resp.Error.Message)
			if resp.Error.Detail != "" {
				fmt.Fprintf(os.Stderr, "Detail: %s\n", resp.Error.Detail)
			}
		} else {
			data, _ := json.MarshalIndent(resp.Data, "", "  ")
			fmt.Println(string(data))
		}
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

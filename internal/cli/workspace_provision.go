package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceProvisionCmd(store domain.StateStore, activityLog *domain.ActivityLog, workspaceRoot *string) *cobra.Command {
	return &cobra.Command{
		Use:   "provision <spec-json>",
		Short: "Provision a workspace from a JSON spec",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var spec domain.WorkspaceSpec
			if err := json.Unmarshal([]byte(args[0]), &spec); err != nil {
				resp := domain.ErrorResponse("workspace.provision", domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "invalid JSON spec",
					Detail:  err.Error(),
				})
				return outputResponse(resp, 1)
			}

			// Resolve workspace root: flag > env > default
			wsRoot := *workspaceRoot
			if wsRoot == "" {
				wsRoot = domain.ResolveWorkspaceRoot(spec.Owner)
			}

			params := domain.ProvisionParams{
				Spec:          spec,
				WorkspaceRoot: wsRoot,
			}

			provisioner := &domain.Provisioner{
				ActivityLog: activityLog,
				// IDE startup is deferred to the ide-worktree-scoping story
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

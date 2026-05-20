package cli

import (
	"fmt"
	"os"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceInspectCmd(store domain.StateStore) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <name>",
		Short: "Inspect a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			instances, err := store.Load()
			if err != nil {
				resp := domain.ErrorResponse("workspace.inspect", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			inst, ok := instances[name]
			if !ok {
				resp := domain.ErrorResponse("workspace.inspect", domain.ErrorInfo{
					Code:    domain.ErrNotFound,
					Message: fmt.Sprintf("workspace %q not found", name),
				})
				return outputResponse(resp, 1)
			}

			enrichLiveness(inst)

			if jsonOutput {
				resp := domain.OkResponse("workspace.inspect", inst)
				return outputResponse(resp, 0)
			}

			fmt.Fprintf(os.Stdout, "Name:             %s\n", inst.Spec.Name)
			fmt.Fprintf(os.Stdout, "State:            %s\n", inst.State)
			fmt.Fprintf(os.Stdout, "Repo:             %s\n", inst.Spec.VCS.Repo)
			fmt.Fprintf(os.Stdout, "Branch:           %s\n", inst.Spec.VCS.Branch)
			fmt.Fprintf(os.Stdout, "Project Root:     %s\n", inst.Spec.ProjectRoot)
			fmt.Fprintf(os.Stdout, "Clone Exists:     %t\n", inst.CloneExists)
			fmt.Fprintf(os.Stdout, "Credential Host:  %s\n", inst.CredentialHost)
			fmt.Fprintf(os.Stdout, "Credential Fresh: %t\n", inst.CredentialFresh)
			if inst.ProvisionedAt != nil {
				fmt.Fprintf(os.Stdout, "Provisioned At:   %s\n", inst.ProvisionedAt.Format("2006-01-02T15:04:05Z"))
			}
			if inst.LastError != nil {
				fmt.Fprintf(os.Stdout, "Last Error:       %s\n", *inst.LastError)
			} else {
				fmt.Fprintf(os.Stdout, "Last Error:       <none>\n")
			}
			return nil
		},
	}
}

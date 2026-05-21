package cli

import (
	"fmt"
	"os"
	"strings"

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

			// Enumerate worktrees from the bare clone (inspect-only, not list)
			worktrees, _ := domain.ListWorktrees(inst.Spec.BareRoot, inst.Spec.Owner)
			inspectData := domain.WorkspaceInspectData{
				WorkspaceInstance: *inst,
				BareRoot:         inst.Spec.BareRoot,
				WorktreeCount:    len(worktrees),
				Worktrees:        worktrees,
				CredFresh:        inst.CredentialFresh,
			}

			if jsonOutput {
				resp := domain.OkResponse("workspace.inspect", inspectData)
				return outputResponse(resp, 0)
			}

			credStatus := "stale"
			if inspectData.CredFresh {
				credStatus = "fresh"
			}

			fmt.Fprintf(os.Stdout, "Name:            %s\n", inst.Spec.Name)
			fmt.Fprintf(os.Stdout, "Worktree:        %s\n", inst.Spec.WorktreeName)
			fmt.Fprintf(os.Stdout, "Repo:            %s\n", inst.Spec.VCS.Repo)
			fmt.Fprintf(os.Stdout, "Branch:          %s\n", inst.Spec.VCS.Branch)
			fmt.Fprintf(os.Stdout, "Project Root:    %s\n", inst.Spec.ProjectRoot)
			fmt.Fprintf(os.Stdout, "Bare Root:       %s\n", inspectData.BareRoot)
			fmt.Fprintf(os.Stdout, "State:           %s\n", inst.State)
			fmt.Fprintf(os.Stdout, "Status:          %s\n", inst.Status)
			fmt.Fprintf(os.Stdout, "Head Commit:     %s\n", inst.HeadCommit)
			fmt.Fprintf(os.Stdout, "Credential:      %s (%s)\n", inst.CredentialHost, credStatus)
			fmt.Fprintf(os.Stdout, "Worktree Count:  %d\n", inspectData.WorktreeCount)
			if len(inspectData.Worktrees) > 0 {
				fmt.Fprintf(os.Stdout, "Worktrees:       %s\n", strings.Join(inspectData.Worktrees, ", "))
			}
			if inst.LastSyncedAt != nil {
				fmt.Fprintf(os.Stdout, "Last Synced:     %s\n", inst.LastSyncedAt.Format("2006-01-02T15:04:05Z"))
			}
			if inst.ProvisionedAt != nil {
				fmt.Fprintf(os.Stdout, "Provisioned:     %s\n", inst.ProvisionedAt.Format("2006-01-02T15:04:05Z"))
			}
			if inst.LastError != nil {
				fmt.Fprintf(os.Stdout, "Last Error:      %s\n", *inst.LastError)
			}
			return nil
		},
	}
}

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

			// Enumerate worktrees from the bare clone (inspect-only, not list)
			worktrees, _ := domain.ListWorktrees(inst.Spec.BareRoot, inst.Spec.Owner)
			inspectData := domain.WorkspaceInspectData{
				WorkspaceInstance: *inst,
				BareRoot:         inst.Spec.BareRoot,
				WorktreeCount:    len(worktrees),
				Worktrees:        worktrees,
				IDEInfo:          domain.IDEInfoFromInstance(inst.IDE),
				TemplateRepo:     domain.ResolveTemplateRepo(inst.Spec.BareRoot, inst.Spec.Owner),
			}

			if jsonOutput {
				resp := domain.OkResponse("workspace.inspect", inspectData)
				return outputResponse(resp, 0)
			}

			fmt.Fprintf(os.Stdout, "Name:            %s\n", inst.Spec.Name)
			fmt.Fprintf(os.Stdout, "Worktree:        %s\n", inst.Spec.WorktreeName)
			fmt.Fprintf(os.Stdout, "Repo:            %s\n", inst.Spec.VCS.Repo)
			fmt.Fprintf(os.Stdout, "Branch:          %s\n", inst.Spec.VCS.Branch)
			fmt.Fprintf(os.Stdout, "Project Root:    %s\n", inst.Spec.ProjectRoot)
			fmt.Fprintf(os.Stdout, "Bare Root:       %s\n", inspectData.BareRoot)
			fmt.Fprintf(os.Stdout, "Lifecycle:       %s\n", inst.Status)
			fmt.Fprintf(os.Stdout, "Head Commit:     %s\n", inst.HeadCommit)
			if inspectData.TemplateRepo != "" {
				fmt.Fprintf(os.Stdout, "Template:        %s\n", inspectData.TemplateRepo)
			}
			fmt.Fprintf(os.Stdout, "Worktree Count:  %d\n", inspectData.WorktreeCount)
			if len(inspectData.Worktrees) > 0 {
				fmt.Fprintf(os.Stdout, "Worktrees:       %s\n", strings.Join(inspectData.Worktrees, ", "))
			}
			if inst.IDE != nil {
				fmt.Fprintf(os.Stdout, "IDE:             %s (port %d, %s)\n", inst.IDE.Adapter, inst.IDE.Port, inst.IDE.Status)
			}
			if len(inst.Events) > 0 {
				fmt.Fprintf(os.Stdout, "Events:\n")
				start := 0
				if len(inst.Events) > 10 {
					start = len(inst.Events) - 10
				}
				for _, ev := range inst.Events[start:] {
					ts := ev.Timestamp.Format("2006-01-02T15:04:05Z")
					if ev.Detail != "" {
						fmt.Fprintf(os.Stdout, "  %s  %s  (%s)\n", ts, ev.Event, ev.Detail)
					} else {
						fmt.Fprintf(os.Stdout, "  %s  %s\n", ts, ev.Event)
					}
				}
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

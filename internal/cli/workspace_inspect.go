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

			ws, ok := instances[name]
			if !ok {
				resp := domain.ErrorResponse("workspace.inspect", domain.ErrorInfo{
					Code:    domain.ErrNotFound,
					Message: fmt.Sprintf("workspace %q not found", name),
				})
				return outputResponse(resp, 1)
			}

			// Build IDE info map from aggregate
			var ideInfoMap map[string]*domain.IDEInfo
			if len(ws.IDE) > 0 {
				ideInfoMap = make(map[string]*domain.IDEInfo, len(ws.IDE))
				for wtName, ide := range ws.IDE {
					ideInfoMap[wtName] = domain.IDEInfoFromInstance(ide)
				}
			}

			inspectData := domain.WorkspaceInspectData{
				Workspace:     *ws,
				WorktreeCount: len(ws.Worktrees),
				IDEInfo:       ideInfoMap,
				TemplateRepo:  domain.ResolveTemplateRepo(ws.BareRoot, ws.Owner),
			}

			if jsonOutput {
				resp := domain.OkResponse("workspace.inspect", inspectData)
				return outputResponse(resp, 0)
			}

			fmt.Fprintf(os.Stdout, "Name:            %s\n", ws.Name)
			fmt.Fprintf(os.Stdout, "Repo:            %s/%s\n", ws.Repo.Host, ws.Repo.Slug)
			fmt.Fprintf(os.Stdout, "Bare Root:       %s\n", ws.BareRoot)
			fmt.Fprintf(os.Stdout, "Lifecycle:       %s\n", ws.Status)
			if inspectData.TemplateRepo != "" {
				fmt.Fprintf(os.Stdout, "Template:        %s\n", inspectData.TemplateRepo)
			}
			fmt.Fprintf(os.Stdout, "Worktree Count:  %d\n", inspectData.WorktreeCount)

			if len(ws.Worktrees) > 0 {
				fmt.Fprintf(os.Stdout, "\nWorktrees:\n")
				fmt.Fprintf(os.Stdout, "  %-16s %-16s %s\n", "NAME", "BRANCH", "PROJECT ROOT")
				for _, wt := range ws.Worktrees {
					fmt.Fprintf(os.Stdout, "  %-16s %-16s %s\n", wt.Name, wt.Branch, wt.ProjectRoot)
				}
			}

			if len(ws.IDE) > 0 {
				fmt.Fprintf(os.Stdout, "\nIDE:\n")
				fmt.Fprintf(os.Stdout, "  %-16s %-20s %-8s %s\n", "WORKTREE", "ADAPTER", "PORT", "STATUS")
				for wtName, ide := range ws.IDE {
					fmt.Fprintf(os.Stdout, "  %-16s %-20s %-8d %s\n", wtName, ide.Adapter, ide.Port, ide.Status)
				}
			}

			if len(ws.Events) > 0 {
				fmt.Fprintf(os.Stdout, "\nEvents:\n")
				start := 0
				if len(ws.Events) > 10 {
					start = len(ws.Events) - 10
				}
				for _, ev := range ws.Events[start:] {
					ts := ev.Timestamp.Format("2006-01-02T15:04:05Z")
					if ev.Detail != "" {
						fmt.Fprintf(os.Stdout, "  %s  %s  (%s)\n", ts, ev.Event, ev.Detail)
					} else {
						fmt.Fprintf(os.Stdout, "  %s  %s\n", ts, ev.Event)
					}
				}
			}
			if ws.LastSyncedAt != nil {
				fmt.Fprintf(os.Stdout, "Last Synced:     %s\n", ws.LastSyncedAt.Format("2006-01-02T15:04:05Z"))
			}
			if ws.ProvisionedAt != nil {
				fmt.Fprintf(os.Stdout, "Provisioned:     %s\n", ws.ProvisionedAt.Format("2006-01-02T15:04:05Z"))
			}
			if ws.LastError != nil {
				fmt.Fprintf(os.Stdout, "Last Error:      %s\n", *ws.LastError)
			}
			return nil
		},
	}
}

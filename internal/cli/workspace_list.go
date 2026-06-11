package cli

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceListCmd(store domain.StateStore) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			instances, err := store.Load()
			if err != nil {
				resp := domain.ErrorResponse("workspace.list", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			list := sortedInstances(instances)

			if jsonOutput {
				items := make([]domain.WorkspaceListItem, len(list))
				for i, ws := range list {
					items[i] = domain.WorkspaceListItemFromInstance(ws)
				}
				resp := domain.OkResponse("workspace.list", items)
				return outputResponse(resp, 0)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tREPO\tWORKTREES\tIDE")
			for _, ws := range list {
				ideCol := ""
				if defWt := ws.DefaultWorktree(); defWt != nil {
					if ide := ws.IDEForWorktree(defWt.Name); ide != nil && ide.Status == domain.StatusReady {
						ideCol = fmt.Sprintf(":%d", ide.Port)
					}
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					ws.Name, ws.Status, ws.Repo.Slug, len(ws.Worktrees), ideCol)
			}
			w.Flush()
			return nil
		},
	}
}

func sortedInstances(instances map[string]*domain.Workspace) []*domain.Workspace {
	list := make([]*domain.Workspace, 0, len(instances))
	for _, ws := range instances {
		list = append(list, ws)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

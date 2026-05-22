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
				resp := domain.OkResponse("workspace.list", list)
				return outputResponse(resp, 0)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tLIFECYCLE\tREPO\tBRANCH")
			for _, inst := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					inst.Spec.Name, inst.Lifecycle, inst.Spec.VCS.Repo, inst.Spec.VCS.Branch)
			}
			w.Flush()
			return nil
		},
	}
}

func sortedInstances(instances map[string]*domain.WorkspaceInstance) []*domain.WorkspaceInstance {
	list := make([]*domain.WorkspaceInstance, 0, len(instances))
	for _, inst := range instances {
		list = append(list, inst)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Spec.Name < list[j].Spec.Name
	})
	return list
}

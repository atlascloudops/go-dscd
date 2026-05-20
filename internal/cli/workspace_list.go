package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

			list := enrichInstances(instances)

			if jsonOutput {
				resp := domain.OkResponse("workspace.list", list)
				return outputResponse(resp, 0)
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATE\tREPO\tBRANCH")
			for _, inst := range list {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					inst.Spec.Name, inst.State, inst.Spec.VCS.Repo, inst.Spec.VCS.Branch)
			}
			w.Flush()
			return nil
		},
	}
}

func enrichInstances(instances map[string]*domain.WorkspaceInstance) []*domain.WorkspaceInstance {
	list := make([]*domain.WorkspaceInstance, 0, len(instances))
	for _, inst := range instances {
		enrichLiveness(inst)
		list = append(list, inst)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Spec.Name < list[j].Spec.Name
	})
	return list
}

func enrichLiveness(inst *domain.WorkspaceInstance) {
	// Check clone exists
	gitDir := filepath.Join(inst.Spec.ProjectRoot, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		inst.CloneExists = true
	} else {
		inst.CloneExists = false
		if inst.State == domain.StateReady {
			inst.State = domain.StateError
			msg := "clone directory missing"
			inst.LastError = &msg
		}
	}

	// Check credential freshness
	credPath := filepath.Join("/home", inst.Spec.Owner, ".config/dsc/credentials/git-credentials")
	data, err := os.ReadFile(credPath)
	if err == nil && strings.Contains(string(data), inst.Spec.VCS.Host) {
		inst.CredentialFresh = true
	} else {
		inst.CredentialFresh = false
	}
}

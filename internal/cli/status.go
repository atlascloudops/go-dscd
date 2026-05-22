package cli

import (
	"fmt"
	"os"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

type StatusData struct {
	DscdVersion      string         `json:"dscd_version"`
	StateFile        string         `json:"state_file"`
	StateFileExists  bool           `json:"state_file_exists"`
	StateFileSizeB   int64          `json:"state_file_size_bytes"`
	WorkspaceCount   int            `json:"workspace_count"`
	WorkspaceSummary map[string]int `json:"workspace_summary"`
	LastSyncedAt  *string        `json:"last_synced_at"`
}

func newStatusCmd(store domain.StateStore, version string, statePath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			data := StatusData{
				DscdVersion: version,
				StateFile:   *statePath,
				WorkspaceSummary: map[string]int{
					"ready":        0,
					"error":        0,
					"pending":      0,
					"provisioning": 0,
				},
			}

			info, err := os.Stat(*statePath)
			if err == nil {
				data.StateFileExists = true
				data.StateFileSizeB = info.Size()
			}

			instances, err := store.Load()
			if err != nil {
				if data.StateFileExists {
					resp := domain.ErrorResponse("status", domain.ErrorInfo{
						Code:    domain.ErrStateCorrupt,
						Message: "state file contains invalid JSON",
						Detail:  err.Error(),
					})
					return outputResponse(resp, 1)
				}
			} else {
				data.WorkspaceCount = len(instances)
				for _, inst := range instances {
					data.WorkspaceSummary[string(inst.Status)]++
					if inst.LastSyncedAt != nil {
						ts := inst.LastSyncedAt.Format("2006-01-02T15:04:05Z")
						data.LastSyncedAt = &ts
					}
				}
			}

			if jsonOutput {
				resp := domain.OkResponse("status", data)
				return outputResponse(resp, 0)
			}

			fmt.Printf("dscd v%s\n", data.DscdVersion)
			fmt.Printf("State file:       %s\n", data.StateFile)
			fmt.Printf("Workspaces:       %d (%d ready, %d error, %d pending)\n",
				data.WorkspaceCount,
				data.WorkspaceSummary["ready"],
				data.WorkspaceSummary["error"],
				data.WorkspaceSummary["pending"])
			if data.LastSyncedAt != nil {
				fmt.Printf("Last sync:   %s\n", *data.LastSyncedAt)
			} else {
				fmt.Printf("Last sync:   <never>\n")
			}
			if data.StateFileExists {
				fmt.Printf("State file size:  %s\n", humanSize(data.StateFileSizeB))
			}
			return nil
		},
	}
}

func humanSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	kb := float64(bytes) / 1024
	return fmt.Sprintf("%.1f KB", kb)
}

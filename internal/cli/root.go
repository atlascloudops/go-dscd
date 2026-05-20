package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jsonOutput bool

func NewRootCommand(version string) *cobra.Command {
	root := &cobra.Command{
		Use:   "dscd",
		Short: "Daemon for workspace lifecycle management",
		Long:  "dscd manages workspace provisioning, reconciliation, and status on a pod.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	root.Version = version
	root.SetVersionTemplate(fmt.Sprintf("dscd v%s\n", version))

	workspace := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("dscd is running")
			return nil
		},
	}

	root.AddCommand(workspace, status)
	return root
}

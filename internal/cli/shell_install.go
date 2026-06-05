package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/atlascloudops/go-dscd/internal/infrastructure"
	"github.com/spf13/cobra"
)

// ShellInstallResult holds the outcome of a shell install operation.
type ShellInstallResult struct {
	Installed bool   `json:"installed"`
	Owner     string `json:"owner"`
}

func newShellInstallCmd() *cobra.Command {
	var owner string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install shell environment hooks for bash, zsh, and fish",
		Long:  "Creates the managed shell directory and installs sourcing hooks for all supported shells (bash, zsh, fish).",
		RunE: func(cmd *cobra.Command, args []string) error {
			const cmdName = "shell.install"

			if owner == "" {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "--owner is required",
				})
				return outputResponse(resp, 1)
			}

			sc := infrastructure.NewShellConfigurator()
			if err := sc.Install(owner); err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("install shell hooks: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			result := ShellInstallResult{
				Installed: true,
				Owner:     owner,
			}

			if jsonOutput {
				resp := domain.OkResponse(cmdName, result)
				return outputResponse(resp, 0)
			}

			fmt.Printf("Shell hooks installed for %s\n", owner)
			return nil
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username to install shell hooks for")
	return cmd
}

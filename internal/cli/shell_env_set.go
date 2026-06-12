package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/atlascloudops/go-dscd/internal/infrastructure"
	"github.com/spf13/cobra"
)

// ShellEnvSetResult holds the outcome of a shell env set operation.
type ShellEnvSetResult struct {
	KeysWritten int `json:"keys_written"`
}

func newShellEnvSetCmd() *cobra.Command {
	var owner string

	cmd := &cobra.Command{
		Use:   "set",
		Short: "Set environment variables in managed shell env files",
		Long:  "Reads a JSON object (map of key-value strings) from stdin and writes the variables to the managed env.sh and env.fish files.",
		RunE: func(cmd *cobra.Command, args []string) error {
			const cmdName = "shell.env.set"

			if owner == "" {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "--owner is required",
				})
				return outputResponse(resp, 1)
			}

			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: fmt.Sprintf("read stdin: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			var env map[string]string
			if err := json.Unmarshal(data, &env); err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: fmt.Sprintf("invalid JSON: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			if len(env) == 0 {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "empty environment map",
				})
				return outputResponse(resp, 1)
			}

			sc := infrastructure.NewShellConfigurator()
			if err := sc.SetEnvironment(owner, env); err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("set environment: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			result := ShellEnvSetResult{
				KeysWritten: len(env),
			}

			if jsonOutput {
				resp := domain.OkResponse(cmdName, result)
				return outputResponse(resp, 0)
			}

			fmt.Printf("Environment: %d keys set for %s\n", len(env), owner)
			return nil
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username to set environment for")
	return cmd
}

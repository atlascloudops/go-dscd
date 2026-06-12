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

// HandshakeSyncPayload is the composed JSON payload for handshake sync.
// Each section is optional — omitted sections are skipped.
type HandshakeSyncPayload struct {
	GitCredentials []string              `json:"git_credentials,omitempty"`
	Sso            *domain.SsoWritePayload `json:"sso,omitempty"`
	Env            map[string]string     `json:"env,omitempty"`
}

// HandshakeSyncData is the composed response for handshake sync.
// Only sections that were present in the input are included.
type HandshakeSyncData struct {
	Git *GitCredentialsWriteResult `json:"git,omitempty"`
	Sso *SsoWriteResult           `json:"sso,omitempty"`
	Env *ShellEnvSetResult        `json:"env,omitempty"`
}

func newHandshakeSyncCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	var owner string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync credentials and environment in one operation",
		Long:  "Reads a composed JSON payload from stdin with optional git_credentials, sso, and env sections. Each section is processed independently.",
		RunE: func(cmd *cobra.Command, args []string) error {
			const cmdName = "handshake.sync"

			if owner == "" {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "--owner is required",
				})
				return outputResponse(resp, 1)
			}

			// Read JSON payload from stdin
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: fmt.Sprintf("read stdin: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			var payload HandshakeSyncPayload
			if err := json.Unmarshal(data, &payload); err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: fmt.Sprintf("invalid JSON payload: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			result := HandshakeSyncData{}
			var errors []string

			// Section 1: Git credentials
			if payload.GitCredentials != nil {
				gitResult, err := writeGitCredentials(owner, payload.GitCredentials, store, activityLog)
				if err != nil {
					errors = append(errors, fmt.Sprintf("git: %s", err.Error()))
				} else {
					result.Git = gitResult
				}
			}

			// Section 2: SSO credentials
			if payload.Sso != nil {
				ssoResult, err := writeSsoCredentials(owner, *payload.Sso, store, activityLog)
				if err != nil {
					errors = append(errors, fmt.Sprintf("sso: %s", err.Error()))
				} else {
					result.Sso = ssoResult
				}
			}

			// Section 3: Environment variables
			if payload.Env != nil && len(payload.Env) > 0 {
				sc := infrastructure.NewShellConfigurator()
				if err := sc.SetEnvironment(owner, payload.Env); err != nil {
					errors = append(errors, fmt.Sprintf("env: %s", err.Error()))
				} else {
					result.Env = &ShellEnvSetResult{
						KeysWritten: len(payload.Env),
					}
				}
			}

			// If all sections failed, return error
			if len(errors) > 0 && result.Git == nil && result.Sso == nil && result.Env == nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("all sections failed: %s", joinErrors(errors)),
				})
				return outputResponse(resp, 1)
			}

			resp := domain.OkResponse(cmdName, result)
			return outputResponse(resp, 0)
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username that owns the credential files")
	return cmd
}

// joinErrors joins error strings with "; ".
func joinErrors(errs []string) string {
	result := ""
	for i, e := range errs {
		if i > 0 {
			result += "; "
		}
		result += e
	}
	return result
}

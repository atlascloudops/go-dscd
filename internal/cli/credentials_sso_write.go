package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/atlascloudops/go-dscd/internal/infrastructure"
	"github.com/spf13/cobra"
)

// SsoWriteResult holds the outcome of an SSO credentials write operation.
type SsoWriteResult struct {
	ProfilesWritten int    `json:"profiles_written"`
	TokenCached     bool   `json:"token_cached"`
	ActiveProfile   string `json:"active_profile"`
}

func newCredentialsSsoWriteCmd() *cobra.Command {
	var owner string

	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write SSO credentials from stdin JSON",
		Long:  "Reads an SsoWritePayload JSON from stdin and writes AWS config, token cache, and active profile.",
		RunE: func(cmd *cobra.Command, args []string) error {
			const cmdName = "credentials.sso.write"

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
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("reading stdin: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			var payload domain.SsoWritePayload
			if err := json.Unmarshal(data, &payload); err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: fmt.Sprintf("invalid JSON payload: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			// 1. Write AWS config (session + profiles)
			if err := domain.WriteAwsConfig(owner, payload.Session, payload.Profiles); err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("write aws config: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			// 2. Write token cache
			cachePath := domain.SsoTokenCachePath(owner, payload.Session.SessionName)
			if err := domain.WriteSsoTokenCache(cachePath, payload.Session, payload.Token); err != nil {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("write token cache: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			// 3. Inject AWS_PROFILE via ShellConfigurator
			if payload.ActiveProfile != "" {
				sc := infrastructure.NewShellConfigurator()
				if err := sc.SetEnvironment(owner, map[string]string{
					"AWS_PROFILE": payload.ActiveProfile,
				}); err != nil {
					resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
						Code:    domain.ErrStateCorrupt,
						Message: fmt.Sprintf("set AWS_PROFILE: %s", err.Error()),
					})
					return outputResponse(resp, 1)
				}
			}

			// 4. Best-effort chown on written files
			chownSsoFiles(owner, cachePath)

			result := SsoWriteResult{
				ProfilesWritten: len(payload.Profiles),
				TokenCached:     true,
				ActiveProfile:   payload.ActiveProfile,
			}

			if jsonOutput {
				resp := domain.OkResponse(cmdName, result)
				return outputResponse(resp, 0)
			}

			fmt.Printf("Profiles written: %d\n", result.ProfilesWritten)
			fmt.Printf("Token cached: %v\n", result.TokenCached)
			if result.ActiveProfile != "" {
				fmt.Printf("Active profile: %s\n", result.ActiveProfile)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username that owns the credential files")
	return cmd
}

// chownSsoFiles sets ownership of SSO-related files to the given user.
// This is best-effort — errors are silently ignored.
func chownSsoFiles(owner, cachePath string) {
	_ = exec.Command("chown", owner+":"+owner, cachePath).Run()
	configPath := fmt.Sprintf("/home/%s/.aws/config", owner)
	_ = exec.Command("chown", owner+":"+owner, configPath).Run()
	awsDir := fmt.Sprintf("/home/%s/.aws", owner)
	_ = exec.Command("chown", "-R", owner+":"+owner, awsDir).Run()
}

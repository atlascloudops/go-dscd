package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newCredentialsSsoStatusCmd() *cobra.Command {
	var owner string
	var sessionName string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check SSO token cache status",
		Long:  "Reads the SSO token cache for a session and reports whether a valid token exists.",
		RunE: func(cmd *cobra.Command, args []string) error {
			const cmdName = "credentials.sso.status"

			if owner == "" {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "--owner is required",
				})
				return outputResponse(resp, 1)
			}

			if sessionName == "" {
				sessionName = "dsc" // default session name
			}

			status := domain.ReadSsoTokenStatus(owner, sessionName)

			if jsonOutput {
				resp := domain.OkResponse(cmdName, status)
				return outputResponse(resp, 0)
			}

			fmt.Printf("Session:   %s\n", status.SessionName)
			fmt.Printf("Has token: %v\n", status.HasToken)
			fmt.Printf("Expired:   %v\n", status.Expired)
			if status.ExpiresAt != "" {
				fmt.Printf("Expires:   %s\n", status.ExpiresAt)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username that owns the credential files")
	cmd.Flags().StringVar(&sessionName, "session", "dsc", "SSO session name to check")
	return cmd
}

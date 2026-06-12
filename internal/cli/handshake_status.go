package cli

import (
	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

// HandshakeStatusData is the composed response for handshake status.
type HandshakeStatusData struct {
	GitFingerprints map[string]string  `json:"git_fingerprints"`
	SsoToken        domain.SsoTokenStatus `json:"sso_token"`
}

func newHandshakeStatusCmd() *cobra.Command {
	var owner string
	var sessionName string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Report credential status for sync handshake",
		Long:  "Composes git credential fingerprints and SSO token status into a single response for the sync handshake protocol.",
		RunE: func(cmd *cobra.Command, args []string) error {
			const cmdName = "handshake.status"

			if owner == "" {
				resp := domain.ErrorResponse(cmdName, domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "--owner is required",
				})
				return outputResponse(resp, 1)
			}

			// Git fingerprints — graceful degradation on error
			path := domain.GitCredentialFilePath(owner)
			fingerprints, err := domain.ParseGitCredentialFile(path)
			if err != nil {
				fingerprints = map[string]string{}
			}
			if fingerprints == nil {
				fingerprints = map[string]string{}
			}

			// SSO token status
			ssoStatus := domain.ReadSsoTokenStatus(owner, sessionName)

			data := HandshakeStatusData{
				GitFingerprints: fingerprints,
				SsoToken:        ssoStatus,
			}

			resp := domain.OkResponse(cmdName, data)
			return outputResponse(resp, 0)
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username that owns the credential files")
	cmd.Flags().StringVar(&sessionName, "session", "dsc", "SSO session name to check")
	return cmd
}

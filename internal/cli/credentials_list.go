package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newCredentialsListCmd() *cobra.Command {
	var owner string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List credential fingerprints by host",
		RunE: func(cmd *cobra.Command, args []string) error {
			if owner == "" {
				resp := domain.ErrorResponse("credentials.list", domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "--owner is required",
				})
				return outputResponse(resp, 1)
			}

			path := domain.CredentialFilePath(owner)
			fingerprints, err := domain.ParseCredentialFile(path)
			if err != nil {
				resp := domain.ErrorResponse("credentials.list", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			if jsonOutput {
				resp := domain.OkResponse("credentials.list", fingerprints)
				return outputResponse(resp, 0)
			}

			if len(fingerprints) == 0 {
				fmt.Println("No credentials found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "HOST\tFINGERPRINT")
			for host, fp := range fingerprints {
				fmt.Fprintf(w, "%s\t%s\n", host, fp)
			}
			w.Flush()
			return nil
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username that owns the credential file")
	return cmd
}

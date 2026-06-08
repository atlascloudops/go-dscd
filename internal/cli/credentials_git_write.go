package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

// GitCredentialsWriteResult holds the outcome of a git credentials write operation.
type GitCredentialsWriteResult struct {
	Updated []string `json:"updated"`
	Added   []string `json:"added"`
}

func newCredentialsGitWriteCmd(store domain.StateStore, activityLog *domain.ActivityLog) *cobra.Command {
	var owner string

	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write git credentials from stdin",
		Long:  "Reads credential lines from stdin (one per line, format: https://{auth_user}:{token}@{host}) and upserts them into the git credential file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if owner == "" {
				resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "--owner is required",
				})
				return outputResponse(resp, 1)
			}

			// Read credential lines from stdin
			var entries []domain.GitCredentialEntry
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				// Parse the line using the same format as git-credentials
				if !strings.HasPrefix(line, "https://") {
					resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
						Code:    domain.ErrSpecInvalid,
						Message: fmt.Sprintf("invalid credential line (must start with https://): %s", line),
					})
					return outputResponse(resp, 1)
				}
				rest := strings.TrimPrefix(line, "https://")
				atIdx := strings.LastIndex(rest, "@")
				if atIdx < 0 {
					resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
						Code:    domain.ErrSpecInvalid,
						Message: fmt.Sprintf("invalid credential line (missing @host): %s", line),
					})
					return outputResponse(resp, 1)
				}
				userinfo := rest[:atIdx]
				host := rest[atIdx+1:]
				colonIdx := strings.Index(userinfo, ":")
				if colonIdx < 0 || host == "" {
					resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
						Code:    domain.ErrSpecInvalid,
						Message: fmt.Sprintf("invalid credential line: %s", line),
					})
					return outputResponse(resp, 1)
				}
				authUser := userinfo[:colonIdx]
				token := userinfo[colonIdx+1:]
				if authUser == "" || token == "" {
					resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
						Code:    domain.ErrSpecInvalid,
						Message: fmt.Sprintf("invalid credential line (empty user or token): %s", line),
					})
					return outputResponse(resp, 1)
				}
				entries = append(entries, domain.GitCredentialEntry{
					Host:     host,
					AuthUser: authUser,
					Token:    token,
				})
			}
			if err := scanner.Err(); err != nil {
				resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("reading stdin: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			if len(entries) == 0 {
				resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: "no credential lines provided on stdin",
				})
				return outputResponse(resp, 1)
			}

			path := domain.GitCredentialFilePath(owner)
			updated, added, err := domain.UpsertGitCredentials(path, entries)
			if err != nil {
				resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			// Best-effort chown to the target user
			chownGitCredentialFile(path, owner)

			// Record credential event in state and activity log
			recordGitCredentialEvent(store, activityLog, owner, updated, added, entries)

			result := GitCredentialsWriteResult{
				Updated: updated,
				Added:   added,
			}
			// Ensure non-nil slices in JSON output
			if result.Updated == nil {
				result.Updated = []string{}
			}
			if result.Added == nil {
				result.Added = []string{}
			}

			if jsonOutput {
				resp := domain.OkResponse("credentials.git.write", result)
				return outputResponse(resp, 0)
			}

			if len(added) > 0 {
				fmt.Printf("Added: %s\n", strings.Join(added, ", "))
			}
			if len(updated) > 0 {
				fmt.Printf("Updated: %s\n", strings.Join(updated, ", "))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&owner, "owner", "", "username that owns the credential file")
	return cmd
}

// recordGitCredentialEvent records a git credential event in the daemon state
// and appends it to the activity log.
// This is best-effort — errors are logged but do not fail the write operation.
func recordGitCredentialEvent(s domain.StateStore, al *domain.ActivityLog, owner string, updated, added []string, entries []domain.GitCredentialEntry) {
	_ = s.WithLock(func() error {
		state, err := s.LoadState()
		if err != nil {
			return err
		}

		cs := state.Credentials[owner]
		if cs == nil {
			cs = &domain.CredentialState{Owner: owner}
			state.Credentials[owner] = cs
		}

		// Determine event type: rotated if all hosts were updated, written if any were added
		var event domain.CredentialEvent
		if len(added) == 0 && len(updated) > 0 {
			event = domain.CredEventGitRotated
		} else {
			event = domain.CredEventGitWritten
		}

		// Build host list for detail
		allHosts := make([]string, 0, len(entries))
		for _, e := range entries {
			allHosts = append(allHosts, e.Host)
		}
		detail := strings.Join(allHosts, ", ")

		cs.RecordEvent(event, detail)

		// Append to activity log (best-effort)
		if al != nil && len(cs.Events) > 0 {
			_ = al.Append(cs.Events[len(cs.Events)-1])
		}

		// Update read projection
		cs.GitHosts = allHosts
		now := time.Now().UTC()
		cs.LastSyncedAt = &now

		return s.SaveState(state)
	})
}

// chownGitCredentialFile sets ownership of the git credential file to the given user.
// This is best-effort -- it silently ignores errors (e.g. when not running as root).
func chownGitCredentialFile(path, owner string) {
	// Use chown command for simplicity; avoids user lookup dependency
	_ = exec.Command("chown", owner+":"+owner, path).Run()
}

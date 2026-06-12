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

// parseGitCredentialLines parses credential URL lines into GitCredentialEntry values.
// Each line must be in the format: https://{auth_user}:{token}@{host}
func parseGitCredentialLines(lines []string) ([]domain.GitCredentialEntry, error) {
	var entries []domain.GitCredentialEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "https://") {
			return nil, fmt.Errorf("invalid credential line (must start with https://): %s", line)
		}
		rest := strings.TrimPrefix(line, "https://")
		atIdx := strings.LastIndex(rest, "@")
		if atIdx < 0 {
			return nil, fmt.Errorf("invalid credential line (missing @host): %s", line)
		}
		userinfo := rest[:atIdx]
		host := rest[atIdx+1:]
		colonIdx := strings.Index(userinfo, ":")
		if colonIdx < 0 || host == "" {
			return nil, fmt.Errorf("invalid credential line: %s", line)
		}
		authUser := userinfo[:colonIdx]
		token := userinfo[colonIdx+1:]
		if authUser == "" || token == "" {
			return nil, fmt.Errorf("invalid credential line (empty user or token): %s", line)
		}
		entries = append(entries, domain.GitCredentialEntry{
			Host:     host,
			AuthUser: authUser,
			Token:    token,
		})
	}
	return entries, nil
}

// writeGitCredentials parses credential lines, upserts them, chowns the file,
// and records events. Returns the write result or an error.
func writeGitCredentials(owner string, credentialLines []string, store domain.StateStore, activityLog *domain.ActivityLog) (*GitCredentialsWriteResult, error) {
	entries, err := parseGitCredentialLines(credentialLines)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no credential lines provided")
	}

	path := domain.GitCredentialFilePath(owner)
	updated, added, err := domain.UpsertGitCredentials(path, entries)
	if err != nil {
		return nil, err
	}

	// Best-effort chown to the target user
	chownGitCredentialFile(path, owner)

	// Record credential event in state and activity log
	recordGitCredentialEvent(store, activityLog, owner, updated, added, entries)

	result := &GitCredentialsWriteResult{
		Updated: updated,
		Added:   added,
	}
	if result.Updated == nil {
		result.Updated = []string{}
	}
	if result.Added == nil {
		result.Added = []string{}
	}
	return result, nil
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
			var lines []string
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" {
					lines = append(lines, line)
				}
			}
			if err := scanner.Err(); err != nil {
				resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
					Code:    domain.ErrStateCorrupt,
					Message: fmt.Sprintf("reading stdin: %s", err.Error()),
				})
				return outputResponse(resp, 1)
			}

			result, err := writeGitCredentials(owner, lines, store, activityLog)
			if err != nil {
				resp := domain.ErrorResponse("credentials.git.write", domain.ErrorInfo{
					Code:    domain.ErrSpecInvalid,
					Message: err.Error(),
				})
				return outputResponse(resp, 1)
			}

			if jsonOutput {
				resp := domain.OkResponse("credentials.git.write", result)
				return outputResponse(resp, 0)
			}

			if len(result.Added) > 0 {
				fmt.Printf("Added: %s\n", strings.Join(result.Added, ", "))
			}
			if len(result.Updated) > 0 {
				fmt.Printf("Updated: %s\n", strings.Join(result.Updated, ", "))
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

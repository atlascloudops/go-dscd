package domain

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitCredentialEntry represents a single git-credentials line.
type GitCredentialEntry struct {
	Host     string `json:"host"`
	AuthUser string `json:"auth_user"`
	Token    string `json:"token"`
}

// GitCredentialLine formats the entry as a git-credentials URL line.
func (e GitCredentialEntry) GitCredentialLine() string {
	return fmt.Sprintf("https://%s:%s@%s", e.AuthUser, e.Token, e.Host)
}

// GitCredentialFingerprint computes a truncated SHA-256 fingerprint for a
// credential entry. The output matches the Python-side credential_fingerprint()
// function: sha256("https://{auth_user}:{token}@{host}")[:16].
func GitCredentialFingerprint(authUser, token, host string) string {
	line := fmt.Sprintf("https://%s:%s@%s", authUser, token, host)
	h := sha256.Sum256([]byte(line))
	return fmt.Sprintf("%x", h)[:16]
}

// GitCredentialFilePath returns the canonical credential file path for a user.
func GitCredentialFilePath(owner string) string {
	return filepath.Join("/home", owner, ".config", "dsc", "credentials", "git-credentials")
}

// ParseGitCredentialFile reads a git-credentials file and returns a map of
// host -> fingerprint. Lines that cannot be parsed are silently skipped.
// If the file does not exist, an empty map is returned (not an error).
func ParseGitCredentialFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open credential file: %w", err)
	}
	defer f.Close()

	result := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		entry, ok := parseCredentialLine(line)
		if !ok {
			continue
		}
		result[entry.Host] = GitCredentialFingerprint(entry.AuthUser, entry.Token, entry.Host)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read credential file: %w", err)
	}
	return result, nil
}

// WriteGitCredentialFile atomically writes credential entries to the given path.
// It writes to a temporary file in the same directory and renames into place.
// The resulting file has mode 0600.
func WriteGitCredentialFile(path string, entries []GitCredentialEntry) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".git-credentials-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	var writeErr error
	for _, entry := range entries {
		if _, err := fmt.Fprintln(tmp, entry.GitCredentialLine()); err != nil {
			writeErr = err
			break
		}
	}
	if err := tmp.Close(); err != nil && writeErr == nil {
		writeErr = err
	}
	if writeErr != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write credential file: %w", writeErr)
	}

	if err := os.Chmod(tmpName, 0600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod credential file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename credential file: %w", err)
	}
	return nil
}

// UpsertGitCredentials reads the existing credential file at path, merges the new
// entries by host (replacing existing entries for the same host, appending new
// ones), and atomically writes the result.
func UpsertGitCredentials(path string, newEntries []GitCredentialEntry) (updated []string, added []string, err error) {
	existing, err := readCredentialEntries(path)
	if err != nil {
		return nil, nil, err
	}

	// Index existing entries by host for merge
	byHost := map[string]GitCredentialEntry{}
	var order []string
	for _, e := range existing {
		byHost[e.Host] = e
		order = append(order, e.Host)
	}

	existingHosts := map[string]bool{}
	for _, e := range existing {
		existingHosts[e.Host] = true
	}

	for _, ne := range newEntries {
		if _, exists := byHost[ne.Host]; exists {
			updated = append(updated, ne.Host)
		} else {
			added = append(added, ne.Host)
			order = append(order, ne.Host)
		}
		byHost[ne.Host] = ne
	}

	// Build merged list preserving original order
	merged := make([]GitCredentialEntry, 0, len(order))
	for _, host := range order {
		merged = append(merged, byHost[host])
	}

	if err := WriteGitCredentialFile(path, merged); err != nil {
		return nil, nil, err
	}
	return updated, added, nil
}

// parseCredentialLine parses a line of the form "https://{user}:{token}@{host}".
func parseCredentialLine(line string) (GitCredentialEntry, bool) {
	// Expected format: https://{auth_user}:{token}@{host}
	if !strings.HasPrefix(line, "https://") {
		return GitCredentialEntry{}, false
	}
	rest := strings.TrimPrefix(line, "https://")

	// Split on @ to get userinfo and host
	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		return GitCredentialEntry{}, false
	}
	userinfo := rest[:atIdx]
	host := rest[atIdx+1:]
	if host == "" {
		return GitCredentialEntry{}, false
	}

	// Split userinfo on first : to get auth_user and token
	colonIdx := strings.Index(userinfo, ":")
	if colonIdx < 0 {
		return GitCredentialEntry{}, false
	}
	authUser := userinfo[:colonIdx]
	token := userinfo[colonIdx+1:]
	if authUser == "" || token == "" {
		return GitCredentialEntry{}, false
	}

	return GitCredentialEntry{
		Host:     host,
		AuthUser: authUser,
		Token:    token,
	}, true
}

// readCredentialEntries reads and parses all valid credential entries from a file.
func readCredentialEntries(path string) ([]GitCredentialEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open credential file: %w", err)
	}
	defer f.Close()

	var entries []GitCredentialEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		entry, ok := parseCredentialLine(line)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read credential file: %w", err)
	}
	return entries, nil
}

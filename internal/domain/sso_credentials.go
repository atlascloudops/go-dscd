package domain

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

// SsoSessionEntry represents an AWS SSO session configuration block.
type SsoSessionEntry struct {
	SessionName           string `json:"session_name"`
	SsoStartUrl           string `json:"sso_start_url"`
	SsoRegion             string `json:"sso_region"`
	SsoRegistrationScopes string `json:"sso_registration_scopes"`
}

// SsoTokenEntry represents a cached SSO bearer token.
type SsoTokenEntry struct {
	AccessToken string `json:"accessToken"`
	ExpiresAt   string `json:"expiresAt"`
}

// AwsProfileEntry represents a named AWS CLI profile backed by SSO.
type AwsProfileEntry struct {
	Name      string `json:"name"`
	AccountId string `json:"account_id"`
	RoleName  string `json:"role_name"`
	Region    string `json:"region"`
}

// SsoWritePayload is the JSON payload consumed by `dscd credentials sso write`.
type SsoWritePayload struct {
	Session       SsoSessionEntry   `json:"session"`
	Token         SsoTokenEntry     `json:"token"`
	Profiles      []AwsProfileEntry `json:"profiles"`
	ActiveProfile string            `json:"active_profile"`
}

// SsoTokenStatus is the response returned by `dscd credentials sso status`.
type SsoTokenStatus struct {
	HasToken    bool   `json:"has_token"`
	Expired     bool   `json:"expired"`
	ExpiresAt   string `json:"expires_at"`
	SessionName string `json:"session_name"`
}

// ---------------------------------------------------------------------------
// Token cache path — matches AWS SDK convention
// ---------------------------------------------------------------------------

// SsoTokenCachePath computes the path to the SSO token cache file for a given
// session. The filename is the SHA-1 hex digest of the raw session name string,
// matching the convention used by the AWS CLI for sso-session based configs.
func SsoTokenCachePath(owner, sessionName string) string {
	h := sha1.Sum([]byte(sessionName))
	filename := fmt.Sprintf("%x.json", h)
	return filepath.Join("/home", owner, ".aws", "sso", "cache", filename)
}

// ssoTokenCacheJSON is the on-disk format expected by boto3/awscli.
type ssoTokenCacheJSON struct {
	StartUrl    string `json:"startUrl"`
	Region      string `json:"region"`
	AccessToken string `json:"accessToken"`
	ExpiresAt   string `json:"expiresAt"`
}

// WriteSsoTokenCache writes the SSO token to the cache file in the format
// expected by boto3 and the AWS CLI.
func WriteSsoTokenCache(path string, session SsoSessionEntry, token SsoTokenEntry) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create sso cache directory: %w", err)
	}

	cacheEntry := ssoTokenCacheJSON{
		StartUrl:    session.SsoStartUrl,
		Region:      session.SsoRegion,
		AccessToken: token.AccessToken,
		ExpiresAt:   token.ExpiresAt,
	}

	data, err := json.MarshalIndent(cacheEntry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sso token cache: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".sso-token-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write sso token cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close sso token cache: %w", err)
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod sso token cache: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename sso token cache: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// AWS config merge
// ---------------------------------------------------------------------------

// ssoSessionSectionName returns the INI section header for an SSO session.
func ssoSessionSectionName(sessionName string) string {
	return fmt.Sprintf("[sso-session %s]", sessionName)
}

// profileSectionName returns the INI section header for a named profile.
func profileSectionName(name string) string {
	return fmt.Sprintf("[profile %s]", name)
}

// WriteAwsConfig performs an idempotent merge of an SSO session and its
// profiles into ~/.aws/config. It adds or updates the sso-session block and
// each profile block, and removes stale profiles owned by the session that
// are no longer in the provided list.
func WriteAwsConfig(owner string, session SsoSessionEntry, profiles []AwsProfileEntry) error {
	configPath := filepath.Join("/home", owner, ".aws", "config")
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create aws config directory: %w", err)
	}

	existing, err := readFileOrEmpty(configPath)
	if err != nil {
		return fmt.Errorf("read aws config: %w", err)
	}

	sections := parseIniSections(existing)

	// Upsert the sso-session block
	sessionHeader := ssoSessionSectionName(session.SessionName)
	sessionLines := []string{
		fmt.Sprintf("sso_start_url = %s", session.SsoStartUrl),
		fmt.Sprintf("sso_region = %s", session.SsoRegion),
		fmt.Sprintf("sso_registration_scopes = %s", session.SsoRegistrationScopes),
	}
	sections = upsertSection(sections, sessionHeader, sessionLines)

	// Build set of desired profile names
	desiredProfiles := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		desiredProfiles[profileSectionName(p.Name)] = true
	}

	// Remove stale profiles owned by this session (they reference this sso_session)
	sections = removeStaleProfiles(sections, session.SessionName, desiredProfiles)

	// Upsert each profile
	for _, p := range profiles {
		header := profileSectionName(p.Name)
		lines := []string{
			fmt.Sprintf("sso_session = %s", session.SessionName),
			fmt.Sprintf("sso_account_id = %s", p.AccountId),
			fmt.Sprintf("sso_role_name = %s", p.RoleName),
			fmt.Sprintf("region = %s", p.Region),
		}
		sections = upsertSection(sections, header, lines)
	}

	result := renderSections(sections)

	tmp, err := os.CreateTemp(dir, ".aws-config-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(result); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write aws config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close aws config: %w", err)
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod aws config: %w", err)
	}
	if err := os.Rename(tmpName, configPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename aws config: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Token status
// ---------------------------------------------------------------------------

// ReadSsoTokenStatus reads the token cache for a session and returns its status.
func ReadSsoTokenStatus(owner, sessionName string) SsoTokenStatus {
	cachePath := SsoTokenCachePath(owner, sessionName)

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return SsoTokenStatus{
			HasToken:    false,
			Expired:     true,
			ExpiresAt:   "",
			SessionName: sessionName,
		}
	}

	var cached ssoTokenCacheJSON
	if err := json.Unmarshal(data, &cached); err != nil {
		return SsoTokenStatus{
			HasToken:    false,
			Expired:     true,
			ExpiresAt:   "",
			SessionName: sessionName,
		}
	}

	expired := true
	if cached.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, cached.ExpiresAt)
		if err == nil {
			expired = time.Now().After(expiry)
		}
	}

	return SsoTokenStatus{
		HasToken:    cached.AccessToken != "",
		Expired:     expired,
		ExpiresAt:   cached.ExpiresAt,
		SessionName: sessionName,
	}
}

// ---------------------------------------------------------------------------
// INI section helpers
// ---------------------------------------------------------------------------

// iniSection represents a parsed INI section with its header line and body lines.
type iniSection struct {
	Header string   // e.g. "[profile foo]" or "[sso-session dsc]", empty for preamble
	Lines  []string // body lines (key = value)
}

// parseIniSections splits an INI-style config into sections.
func parseIniSections(content string) []iniSection {
	var sections []iniSection
	var current *iniSection

	// Start with a preamble section for any content before the first header
	preamble := iniSection{Header: ""}
	current = &preamble

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			// Save previous section
			sections = append(sections, *current)
			current = &iniSection{Header: trimmed}
		} else {
			current.Lines = append(current.Lines, line)
		}
	}
	sections = append(sections, *current)

	return sections
}

// upsertSection adds or replaces a section identified by header.
func upsertSection(sections []iniSection, header string, lines []string) []iniSection {
	for i, s := range sections {
		if s.Header == header {
			sections[i].Lines = lines
			return sections
		}
	}
	// Append new section
	return append(sections, iniSection{Header: header, Lines: lines})
}

// removeStaleProfiles removes profile sections that reference the given
// sso_session but are not in the desiredProfiles set.
func removeStaleProfiles(sections []iniSection, sessionName string, desiredProfiles map[string]bool) []iniSection {
	var result []iniSection
	for _, s := range sections {
		if strings.HasPrefix(s.Header, "[profile ") && !desiredProfiles[s.Header] {
			// Check if this profile references our session
			ownsSession := false
			for _, line := range s.Lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == fmt.Sprintf("sso_session = %s", sessionName) {
					ownsSession = true
					break
				}
			}
			if ownsSession {
				continue // Remove this stale profile
			}
		}
		result = append(result, s)
	}
	return result
}

// renderSections serialises INI sections back to a string.
func renderSections(sections []iniSection) string {
	var parts []string
	for _, s := range sections {
		if s.Header == "" {
			// Preamble — only include if it has non-empty lines
			nonEmpty := false
			for _, l := range s.Lines {
				if strings.TrimSpace(l) != "" {
					nonEmpty = true
					break
				}
			}
			if nonEmpty {
				parts = append(parts, strings.Join(s.Lines, "\n"))
			}
		} else {
			block := s.Header + "\n" + strings.Join(s.Lines, "\n")
			parts = append(parts, block)
		}
	}

	result := strings.Join(parts, "\n\n") + "\n"
	return result
}

// readFileOrEmpty reads a file and returns its contents, or empty string if
// the file does not exist.
func readFileOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

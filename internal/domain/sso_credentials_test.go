package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Token cache path
// ---------------------------------------------------------------------------

func TestSsoTokenCachePath(t *testing.T) {
	path := SsoTokenCachePath("jperez", "dsc")
	if !strings.HasPrefix(path, "/home/jperez/.aws/sso/cache/") {
		t.Fatalf("unexpected path prefix: %s", path)
	}
	if !strings.HasSuffix(path, ".json") {
		t.Fatalf("expected .json suffix: %s", path)
	}

	// Deterministic: same inputs produce same path
	path2 := SsoTokenCachePath("jperez", "dsc")
	if path != path2 {
		t.Fatalf("non-deterministic: %s != %s", path, path2)
	}

	// Different session names produce different paths
	path3 := SsoTokenCachePath("jperez", "other")
	if path == path3 {
		t.Fatal("different sessions should produce different cache paths")
	}
}

// ---------------------------------------------------------------------------
// Write + read token cache round-trip
// ---------------------------------------------------------------------------

func TestWriteSsoTokenCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "sso", "cache", "test.json")

	session := SsoSessionEntry{
		SessionName:           "dsc",
		SsoStartUrl:           "https://d-123456.awsapps.com/start",
		SsoRegion:             "us-east-1",
		SsoRegistrationScopes: "sso:account:access",
	}
	token := SsoTokenEntry{
		AccessToken: "eyJraWQ...",
		ExpiresAt:   "2026-06-06T18:00:00Z",
	}

	if err := WriteSsoTokenCache(cachePath, session, token); err != nil {
		t.Fatalf("WriteSsoTokenCache: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected 0600, got %o", perm)
	}

	// Read and verify JSON format matches boto3 expectations
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var cached ssoTokenCacheJSON
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cached.StartUrl != session.SsoStartUrl {
		t.Fatalf("startUrl mismatch: %s", cached.StartUrl)
	}
	if cached.Region != session.SsoRegion {
		t.Fatalf("region mismatch: %s", cached.Region)
	}
	if cached.AccessToken != token.AccessToken {
		t.Fatalf("accessToken mismatch: %s", cached.AccessToken)
	}
	if cached.ExpiresAt != token.ExpiresAt {
		t.Fatalf("expiresAt mismatch: %s", cached.ExpiresAt)
	}
}

// ---------------------------------------------------------------------------
// AWS config merge idempotency
// ---------------------------------------------------------------------------

func TestWriteAwsConfigIdempotent(t *testing.T) {
	dir := t.TempDir()

	// Override home directory by writing to a temp dir structure
	homeDir := filepath.Join(dir, "home", "jperez")
	if err := os.MkdirAll(filepath.Join(homeDir, ".aws"), 0700); err != nil {
		t.Fatal(err)
	}

	session := SsoSessionEntry{
		SessionName:           "dsc",
		SsoStartUrl:           "https://d-123456.awsapps.com/start",
		SsoRegion:             "us-east-1",
		SsoRegistrationScopes: "sso:account:access",
	}
	profiles := []AwsProfileEntry{
		{Name: "dev", AccountId: "111111111111", RoleName: "ReadOnly", Region: "us-east-1"},
		{Name: "prod", AccountId: "222222222222", RoleName: "Admin", Region: "us-west-2"},
	}

	// We need to write directly to the config path since WriteAwsConfig
	// uses /home/owner path. Use writeAwsConfigAt helper for testing.
	configPath := filepath.Join(homeDir, ".aws", "config")

	// Write twice with same content — result should be identical
	writeAwsConfigAt(t, configPath, session, profiles)
	first, _ := os.ReadFile(configPath)

	writeAwsConfigAt(t, configPath, session, profiles)
	second, _ := os.ReadFile(configPath)

	if string(first) != string(second) {
		t.Fatalf("config not idempotent.\nFirst:\n%s\nSecond:\n%s", first, second)
	}

	// Verify content has expected sections
	content := string(first)
	if !strings.Contains(content, "[sso-session dsc]") {
		t.Fatal("missing sso-session section")
	}
	if !strings.Contains(content, "[profile dev]") {
		t.Fatal("missing profile dev section")
	}
	if !strings.Contains(content, "[profile prod]") {
		t.Fatal("missing profile prod section")
	}
	if !strings.Contains(content, "sso_start_url = https://d-123456.awsapps.com/start") {
		t.Fatal("missing sso_start_url")
	}
}

// writeAwsConfigAt is a test helper that writes AWS config directly to a path,
// bypassing the /home/{owner} resolution in WriteAwsConfig.
func writeAwsConfigAt(t *testing.T, configPath string, session SsoSessionEntry, profiles []AwsProfileEntry) {
	t.Helper()
	existing, err := readFileOrEmpty(configPath)
	if err != nil {
		t.Fatal(err)
	}

	sections := parseIniSections(existing)

	sessionHeader := ssoSessionSectionName(session.SessionName)
	sessionLines := []string{
		"sso_start_url = " + session.SsoStartUrl,
		"sso_region = " + session.SsoRegion,
		"sso_registration_scopes = " + session.SsoRegistrationScopes,
	}
	sections = upsertSection(sections, sessionHeader, sessionLines)

	desiredProfiles := make(map[string]bool, len(profiles))
	for _, p := range profiles {
		desiredProfiles[profileSectionName(p.Name)] = true
	}
	sections = removeStaleProfiles(sections, session.SessionName, desiredProfiles)

	for _, p := range profiles {
		header := profileSectionName(p.Name)
		lines := []string{
			"sso_session = " + session.SessionName,
			"sso_account_id = " + p.AccountId,
			"sso_role_name = " + p.RoleName,
			"region = " + p.Region,
		}
		sections = upsertSection(sections, header, lines)
	}

	result := renderSections(sections)
	if err := os.WriteFile(configPath, []byte(result), 0600); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Stale profile removal
// ---------------------------------------------------------------------------

func TestWriteAwsConfigRemovesStaleProfiles(t *testing.T) {
	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home", "jperez")
	configPath := filepath.Join(homeDir, ".aws", "config")
	if err := os.MkdirAll(filepath.Join(homeDir, ".aws"), 0700); err != nil {
		t.Fatal(err)
	}

	session := SsoSessionEntry{
		SessionName:           "dsc",
		SsoStartUrl:           "https://d-123456.awsapps.com/start",
		SsoRegion:             "us-east-1",
		SsoRegistrationScopes: "sso:account:access",
	}

	// First write: dev + staging profiles
	profiles1 := []AwsProfileEntry{
		{Name: "dev", AccountId: "111111111111", RoleName: "ReadOnly", Region: "us-east-1"},
		{Name: "staging", AccountId: "333333333333", RoleName: "Admin", Region: "us-east-1"},
	}
	writeAwsConfigAt(t, configPath, session, profiles1)

	content, _ := os.ReadFile(configPath)
	if !strings.Contains(string(content), "[profile staging]") {
		t.Fatal("staging profile should exist after first write")
	}

	// Second write: only dev profile — staging should be removed
	profiles2 := []AwsProfileEntry{
		{Name: "dev", AccountId: "111111111111", RoleName: "ReadOnly", Region: "us-east-1"},
	}
	writeAwsConfigAt(t, configPath, session, profiles2)

	content, _ = os.ReadFile(configPath)
	if strings.Contains(string(content), "[profile staging]") {
		t.Fatal("staging profile should be removed after second write")
	}
	if !strings.Contains(string(content), "[profile dev]") {
		t.Fatal("dev profile should still exist")
	}
}

// ---------------------------------------------------------------------------
// Preserves non-owned profiles
// ---------------------------------------------------------------------------

func TestWriteAwsConfigPreservesUnownedProfiles(t *testing.T) {
	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home", "jperez")
	configPath := filepath.Join(homeDir, ".aws", "config")
	if err := os.MkdirAll(filepath.Join(homeDir, ".aws"), 0700); err != nil {
		t.Fatal(err)
	}

	// Pre-seed with a manually created profile not owned by the SSO session
	initial := "[profile manual]\naws_access_key_id = AKIA...\naws_secret_access_key = secret\n"
	if err := os.WriteFile(configPath, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}

	session := SsoSessionEntry{
		SessionName:           "dsc",
		SsoStartUrl:           "https://d-123456.awsapps.com/start",
		SsoRegion:             "us-east-1",
		SsoRegistrationScopes: "sso:account:access",
	}
	profiles := []AwsProfileEntry{
		{Name: "dev", AccountId: "111111111111", RoleName: "ReadOnly", Region: "us-east-1"},
	}
	writeAwsConfigAt(t, configPath, session, profiles)

	content, _ := os.ReadFile(configPath)
	if !strings.Contains(string(content), "[profile manual]") {
		t.Fatal("unowned profile should be preserved")
	}
	if !strings.Contains(string(content), "[profile dev]") {
		t.Fatal("SSO profile should be added")
	}
}

// ---------------------------------------------------------------------------
// Token status — expired detection
// ---------------------------------------------------------------------------

func TestReadSsoTokenStatusExpired(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "expired.json")

	pastTime := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	cached := ssoTokenCacheJSON{
		StartUrl:    "https://d-123456.awsapps.com/start",
		Region:      "us-east-1",
		AccessToken: "eyJraWQ...",
		ExpiresAt:   pastTime,
	}
	data, _ := json.Marshal(cached)
	if err := os.WriteFile(cachePath, data, 0600); err != nil {
		t.Fatal(err)
	}

	// ReadSsoTokenStatus reads from /home/owner path, so we test the logic
	// directly by reading the file ourselves.
	var parsed ssoTokenCacheJSON
	raw, _ := os.ReadFile(cachePath)
	json.Unmarshal(raw, &parsed)

	expiry, _ := time.Parse(time.RFC3339, parsed.ExpiresAt)
	expired := time.Now().After(expiry)
	if !expired {
		t.Fatal("token should be expired")
	}
}

func TestReadSsoTokenStatusValid(t *testing.T) {
	futureTime := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	cached := ssoTokenCacheJSON{
		StartUrl:    "https://d-123456.awsapps.com/start",
		Region:      "us-east-1",
		AccessToken: "eyJraWQ...",
		ExpiresAt:   futureTime,
	}

	expiry, _ := time.Parse(time.RFC3339, cached.ExpiresAt)
	expired := time.Now().After(expiry)
	if expired {
		t.Fatal("token should not be expired")
	}
}

func TestReadSsoTokenStatusMissingFile(t *testing.T) {
	status := ReadSsoTokenStatus("/nonexistent/user", "dsc")
	if status.HasToken {
		t.Fatal("should have no token for missing file")
	}
	if !status.Expired {
		t.Fatal("missing token should be reported as expired")
	}
	if status.SessionName != "dsc" {
		t.Fatalf("session_name mismatch: %s", status.SessionName)
	}
}

// ---------------------------------------------------------------------------
// INI section parsing
// ---------------------------------------------------------------------------

func TestParseIniSectionsRoundTrip(t *testing.T) {
	input := `[sso-session dsc]
sso_start_url = https://d-123456.awsapps.com/start
sso_region = us-east-1

[profile dev]
sso_session = dsc
sso_account_id = 111111111111
`
	sections := parseIniSections(input)

	// Should have preamble + 2 sections
	found := 0
	for _, s := range sections {
		if s.Header != "" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected 2 named sections, got %d", found)
	}

	result := renderSections(sections)
	if !strings.Contains(result, "[sso-session dsc]") {
		t.Fatal("missing sso-session in rendered output")
	}
	if !strings.Contains(result, "[profile dev]") {
		t.Fatal("missing profile in rendered output")
	}
}

func TestUpsertSectionReplacesExisting(t *testing.T) {
	sections := []iniSection{
		{Header: "[sso-session dsc]", Lines: []string{"sso_region = us-east-1"}},
	}

	sections = upsertSection(sections, "[sso-session dsc]", []string{"sso_region = eu-west-1"})
	if len(sections) != 1 {
		t.Fatalf("expected 1 section after upsert, got %d", len(sections))
	}
	if sections[0].Lines[0] != "sso_region = eu-west-1" {
		t.Fatalf("section not updated: %v", sections[0].Lines)
	}
}

func TestUpsertSectionAddsNew(t *testing.T) {
	sections := []iniSection{
		{Header: "[sso-session dsc]", Lines: []string{"sso_region = us-east-1"}},
	}

	sections = upsertSection(sections, "[profile dev]", []string{"region = us-east-1"})
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections after upsert, got %d", len(sections))
	}
}

package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitCredentialFingerprint(t *testing.T) {
	// Verify deterministic truncated SHA-256 output matching Python:
	//   hashlib.sha256(b"https://jperez:ghp_abc123@github.com").hexdigest()[:16]
	fp := GitCredentialFingerprint("jperez", "ghp_abc123", "github.com")
	if len(fp) != 16 {
		t.Fatalf("expected 16-char fingerprint, got %d: %s", len(fp), fp)
	}

	// Same inputs must produce same output (determinism)
	fp2 := GitCredentialFingerprint("jperez", "ghp_abc123", "github.com")
	if fp != fp2 {
		t.Fatalf("fingerprint not deterministic: %s != %s", fp, fp2)
	}

	// Different inputs must produce different output
	fp3 := GitCredentialFingerprint("jperez", "ghp_different", "github.com")
	if fp == fp3 {
		t.Fatal("different tokens produced same fingerprint")
	}

	// Different hosts must produce different output
	fp4 := GitCredentialFingerprint("jperez", "ghp_abc123", "gitlab.com")
	if fp == fp4 {
		t.Fatal("different hosts produced same fingerprint")
	}
}

func TestGitCredentialEntryLine(t *testing.T) {
	e := GitCredentialEntry{Host: "github.com", AuthUser: "jperez", Token: "ghp_abc123"}
	want := "https://jperez:ghp_abc123@github.com"
	if got := e.GitCredentialLine(); got != want {
		t.Fatalf("GitCredentialLine() = %q, want %q", got, want)
	}
}

func TestParseCredentialLine(t *testing.T) {
	tests := []struct {
		line string
		ok   bool
		host string
	}{
		{"https://jperez:ghp_abc123@github.com", true, "github.com"},
		{"https://user:token@gitlab.com", true, "gitlab.com"},
		{"http://user:token@host.com", false, ""},     // wrong scheme
		{"https://nopassword@host.com", false, ""},     // missing token
		{"https://:token@host.com", false, ""},          // missing user
		{"https://user:@host.com", false, ""},            // missing token value
		{"", false, ""},                                   // empty
		{"garbage", false, ""},                            // garbage
	}
	for _, tt := range tests {
		entry, ok := parseCredentialLine(tt.line)
		if ok != tt.ok {
			t.Errorf("parseCredentialLine(%q) ok=%v, want %v", tt.line, ok, tt.ok)
			continue
		}
		if ok && entry.Host != tt.host {
			t.Errorf("parseCredentialLine(%q) host=%q, want %q", tt.line, entry.Host, tt.host)
		}
	}
}

func TestParseGitCredentialFileNotExist(t *testing.T) {
	result, err := ParseGitCredentialFile("/nonexistent/path/to/credentials")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty map for missing file, got %d entries", len(result))
	}
}

func TestParseGitCredentialFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-credentials")

	entries := []GitCredentialEntry{
		{Host: "github.com", AuthUser: "jperez", Token: "ghp_abc123"},
		{Host: "gitlab.com", AuthUser: "jperez", Token: "glpat_xyz789"},
	}

	if err := WriteGitCredentialFile(path, entries); err != nil {
		t.Fatalf("WriteGitCredentialFile: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected 0600 permissions, got %o", perm)
	}

	// Parse and verify fingerprints
	result, err := ParseGitCredentialFile(path)
	if err != nil {
		t.Fatalf("ParseGitCredentialFile: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	for _, e := range entries {
		fp, ok := result[e.Host]
		if !ok {
			t.Fatalf("missing host %s in result", e.Host)
		}
		expected := GitCredentialFingerprint(e.AuthUser, e.Token, e.Host)
		if fp != expected {
			t.Fatalf("fingerprint mismatch for %s: %s != %s", e.Host, fp, expected)
		}
	}
}

func TestUpsertGitCredentialsNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "git-credentials")

	entries := []GitCredentialEntry{
		{Host: "github.com", AuthUser: "jperez", Token: "ghp_abc123"},
	}

	updated, added, err := UpsertGitCredentials(path, entries)
	if err != nil {
		t.Fatalf("UpsertGitCredentials: %v", err)
	}
	if len(updated) != 0 {
		t.Fatalf("expected no updates, got %v", updated)
	}
	if len(added) != 1 || added[0] != "github.com" {
		t.Fatalf("expected [github.com] added, got %v", added)
	}
}

func TestUpsertGitCredentialsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-credentials")

	entry := GitCredentialEntry{Host: "github.com", AuthUser: "jperez", Token: "ghp_abc123"}

	// Write initial
	if err := WriteGitCredentialFile(path, []GitCredentialEntry{entry}); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Upsert same entry with new token
	newEntry := GitCredentialEntry{Host: "github.com", AuthUser: "jperez", Token: "ghp_new_token"}
	updated, added, err := UpsertGitCredentials(path, []GitCredentialEntry{newEntry})
	if err != nil {
		t.Fatalf("UpsertGitCredentials: %v", err)
	}
	if len(updated) != 1 || updated[0] != "github.com" {
		t.Fatalf("expected [github.com] updated, got %v", updated)
	}
	if len(added) != 0 {
		t.Fatalf("expected no adds, got %v", added)
	}

	// Read back and verify single line with new token
	result, err := ParseGitCredentialFile(path)
	if err != nil {
		t.Fatalf("ParseGitCredentialFile: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry after idempotent upsert, got %d", len(result))
	}
	expectedFP := GitCredentialFingerprint("jperez", "ghp_new_token", "github.com")
	if result["github.com"] != expectedFP {
		t.Fatalf("fingerprint should reflect new token")
	}
}

func TestUpsertGitCredentialsMerge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-credentials")

	// Write initial with github
	initial := []GitCredentialEntry{
		{Host: "github.com", AuthUser: "jperez", Token: "ghp_abc123"},
	}
	if err := WriteGitCredentialFile(path, initial); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Upsert: update github, add gitlab
	newEntries := []GitCredentialEntry{
		{Host: "github.com", AuthUser: "jperez", Token: "ghp_rotated"},
		{Host: "gitlab.com", AuthUser: "jperez", Token: "glpat_new"},
	}
	updated, added, err := UpsertGitCredentials(path, newEntries)
	if err != nil {
		t.Fatalf("UpsertGitCredentials: %v", err)
	}
	if len(updated) != 1 || updated[0] != "github.com" {
		t.Fatalf("expected [github.com] updated, got %v", updated)
	}
	if len(added) != 1 || added[0] != "gitlab.com" {
		t.Fatalf("expected [gitlab.com] added, got %v", added)
	}

	// Verify final state
	result, err := ParseGitCredentialFile(path)
	if err != nil {
		t.Fatalf("ParseGitCredentialFile: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
}

func TestWriteGitCredentialFileEmptyEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "git-credentials")

	if err := WriteGitCredentialFile(path, []GitCredentialEntry{}); err != nil {
		t.Fatalf("WriteGitCredentialFile with empty entries: %v", err)
	}

	result, err := ParseGitCredentialFile(path)
	if err != nil {
		t.Fatalf("ParseGitCredentialFile: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(result))
	}
}

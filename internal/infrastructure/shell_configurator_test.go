package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestConfigurator returns a ShellConfigurator whose adapters all resolve
// paths against a temp directory instead of /home.
func newTestConfigurator(t *testing.T) (*ShellConfigurator, string) {
	t.Helper()
	tmpHome := t.TempDir()
	return &ShellConfigurator{
		homeDir: tmpHome,
		adapters: []ShellAdapter{
			&BashAdapter{homeDir: tmpHome},
			&ZshAdapter{homeDir: tmpHome},
			&FishAdapter{homeDir: tmpHome},
		},
	}, tmpHome
}

// ownerHome returns the directory that represents ~owner within the test root.
func ownerHome(tmpHome, owner string) string {
	return filepath.Join(tmpHome, owner)
}

// shellDir returns the managed shell config directory for owner within the test root.
func testShellDir(tmpHome, owner string) string {
	return filepath.Join(tmpHome, owner, shellDirRel)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestInstallCreatesDirectoryAndHooks(t *testing.T) {
	sc, tmpHome := newTestConfigurator(t)

	if err := sc.Install("testuser"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Shell directory must exist.
	shellDir := testShellDir(tmpHome, "testuser")
	info, err := os.Stat(shellDir)
	if err != nil {
		t.Fatalf("shell dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("shell dir is not a directory")
	}

	home := ownerHome(tmpHome, "testuser")

	// Bash hook must be in .bashrc.
	assertFileContains(t, filepath.Join(home, ".bashrc"), bashHookMarker)

	// Zsh hook must be in .zshenv.
	assertFileContains(t, filepath.Join(home, ".zshenv"), zshHookMarker)

	// Fish autoload file must exist.
	fishAutoload := filepath.Join(home, ".config", "fish", "conf.d", "dsc-env.fish")
	assertFileContains(t, fishAutoload, managedHeader)
}

func TestInstallIdempotent(t *testing.T) {
	sc, tmpHome := newTestConfigurator(t)

	// Install twice.
	for i := 0; i < 2; i++ {
		if err := sc.Install("testuser"); err != nil {
			t.Fatalf("Install (round %d): %v", i+1, err)
		}
	}

	home := ownerHome(tmpHome, "testuser")

	// Bash hook should appear exactly once.
	content := readFile(t, filepath.Join(home, ".bashrc"))
	count := strings.Count(content, bashHookMarker)
	if count != 1 {
		t.Fatalf("expected 1 bash hook marker, found %d", count)
	}

	// Zsh hook should appear exactly once.
	content = readFile(t, filepath.Join(home, ".zshenv"))
	count = strings.Count(content, zshHookMarker)
	if count != 1 {
		t.Fatalf("expected 1 zsh hook marker, found %d", count)
	}
}

func TestSetEnvironmentFresh(t *testing.T) {
	sc, tmpHome := newTestConfigurator(t)

	env := map[string]string{"AWS_PROFILE": "sandbox-developer"}
	if err := sc.SetEnvironment("testuser", env); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}

	shellDir := testShellDir(tmpHome, "testuser")

	// Verify env.sh
	shContent := readFile(t, filepath.Join(shellDir, envShFilename))
	assertContains(t, shContent, managedHeader)
	assertContains(t, shContent, `export AWS_PROFILE="sandbox-developer"`)

	// Verify env.fish
	fishContent := readFile(t, filepath.Join(shellDir, envFishFilename))
	assertContains(t, fishContent, managedHeader)
	assertContains(t, fishContent, `set -gx AWS_PROFILE "sandbox-developer"`)
}

func TestSetEnvironmentMerge(t *testing.T) {
	sc, tmpHome := newTestConfigurator(t)

	// First write.
	env1 := map[string]string{
		"AWS_PROFILE": "sandbox-developer",
		"AWS_REGION":  "us-east-1",
	}
	if err := sc.SetEnvironment("testuser", env1); err != nil {
		t.Fatalf("SetEnvironment (1): %v", err)
	}

	// Second write: update AWS_PROFILE, add FOO, leave AWS_REGION untouched.
	env2 := map[string]string{
		"AWS_PROFILE": "production-admin",
		"FOO":         "bar",
	}
	if err := sc.SetEnvironment("testuser", env2); err != nil {
		t.Fatalf("SetEnvironment (2): %v", err)
	}

	shellDir := testShellDir(tmpHome, "testuser")
	shContent := readFile(t, filepath.Join(shellDir, envShFilename))

	// Updated key.
	assertContains(t, shContent, `export AWS_PROFILE="production-admin"`)
	// Preserved key.
	assertContains(t, shContent, `export AWS_REGION="us-east-1"`)
	// New key.
	assertContains(t, shContent, `export FOO="bar"`)
	// Old value must not be present.
	assertNotContains(t, shContent, "sandbox-developer")
}

func TestUnsetEnvironment(t *testing.T) {
	sc, tmpHome := newTestConfigurator(t)

	env := map[string]string{
		"AWS_PROFILE": "sandbox-developer",
		"AWS_REGION":  "us-east-1",
		"FOO":         "bar",
	}
	if err := sc.SetEnvironment("testuser", env); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}

	// Unset AWS_PROFILE and FOO.
	if err := sc.UnsetEnvironment("testuser", []string{"AWS_PROFILE", "FOO"}); err != nil {
		t.Fatalf("UnsetEnvironment: %v", err)
	}

	shellDir := testShellDir(tmpHome, "testuser")

	// env.sh should only have AWS_REGION.
	shContent := readFile(t, filepath.Join(shellDir, envShFilename))
	assertNotContains(t, shContent, "AWS_PROFILE")
	assertNotContains(t, shContent, "FOO")
	assertContains(t, shContent, `export AWS_REGION="us-east-1"`)

	// env.fish should only have AWS_REGION.
	fishContent := readFile(t, filepath.Join(shellDir, envFishFilename))
	assertNotContains(t, fishContent, "AWS_PROFILE")
	assertNotContains(t, fishContent, "FOO")
	assertContains(t, fishContent, `set -gx AWS_REGION "us-east-1"`)
}

func TestUnsetEnvironmentNonexistentFile(t *testing.T) {
	sc, _ := newTestConfigurator(t)

	// Unsetting keys from files that do not exist should not error.
	if err := sc.UnsetEnvironment("testuser", []string{"AWS_PROFILE"}); err != nil {
		t.Fatalf("UnsetEnvironment on missing file: %v", err)
	}
}

func TestFilePermissions(t *testing.T) {
	sc, tmpHome := newTestConfigurator(t)

	env := map[string]string{"AWS_PROFILE": "test"}
	if err := sc.SetEnvironment("testuser", env); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}

	shellDir := testShellDir(tmpHome, "testuser")

	// Check directory permissions.
	dirInfo, err := os.Stat(shellDir)
	if err != nil {
		t.Fatalf("stat shell dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != dirPerm {
		t.Fatalf("expected dir perm %o, got %o", dirPerm, perm)
	}

	// Check env.sh permissions.
	shInfo, err := os.Stat(filepath.Join(shellDir, envShFilename))
	if err != nil {
		t.Fatalf("stat env.sh: %v", err)
	}
	if perm := shInfo.Mode().Perm(); perm != filePerm {
		t.Fatalf("expected file perm %o, got %o", filePerm, perm)
	}

	// Check env.fish permissions.
	fishInfo, err := os.Stat(filepath.Join(shellDir, envFishFilename))
	if err != nil {
		t.Fatalf("stat env.fish: %v", err)
	}
	if perm := fishInfo.Mode().Perm(); perm != filePerm {
		t.Fatalf("expected file perm %o, got %o", filePerm, perm)
	}
}

func TestMultiShellWriteConsistency(t *testing.T) {
	sc, tmpHome := newTestConfigurator(t)

	env := map[string]string{
		"AWS_PROFILE": "dev",
		"EDITOR":      "vim",
	}
	if err := sc.SetEnvironment("testuser", env); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}

	shellDir := testShellDir(tmpHome, "testuser")

	shContent := readFile(t, filepath.Join(shellDir, envShFilename))
	fishContent := readFile(t, filepath.Join(shellDir, envFishFilename))

	// Both files should contain both keys.
	for _, key := range []string{"AWS_PROFILE", "EDITOR"} {
		assertContains(t, shContent, key)
		assertContains(t, fishContent, key)
	}
}

func TestParsePosixKey(t *testing.T) {
	tests := []struct {
		line string
		key  string
		ok   bool
	}{
		{`export AWS_PROFILE="sandbox"`, "AWS_PROFILE", true},
		{`export FOO="bar"`, "FOO", true},
		{`# comment`, "", false},
		{`set -gx AWS_PROFILE "sandbox"`, "", false},
		{`export =bad`, "", false},
		{``, "", false},
	}
	for _, tt := range tests {
		key, ok := parsePosixKey(tt.line)
		if ok != tt.ok || key != tt.key {
			t.Errorf("parsePosixKey(%q) = (%q, %v), want (%q, %v)", tt.line, key, ok, tt.key, tt.ok)
		}
	}
}

func TestParseFishKey(t *testing.T) {
	tests := []struct {
		line string
		key  string
		ok   bool
	}{
		{`set -gx AWS_PROFILE "sandbox"`, "AWS_PROFILE", true},
		{`set -gx FOO "bar"`, "FOO", true},
		{`# comment`, "", false},
		{`export AWS_PROFILE="sandbox"`, "", false},
		{``, "", false},
	}
	for _, tt := range tests {
		key, ok := parseFishKey(tt.line)
		if ok != tt.ok || key != tt.key {
			t.Errorf("parseFishKey(%q) = (%q, %v), want (%q, %v)", tt.line, key, ok, tt.key, tt.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertFileContains(t *testing.T, path, substr string) {
	t.Helper()
	content := readFile(t, path)
	if !strings.Contains(content, substr) {
		t.Fatalf("file %s does not contain %q;\ncontent:\n%s", path, substr, content)
	}
}

func assertContains(t *testing.T, content, substr string) {
	t.Helper()
	if !strings.Contains(content, substr) {
		t.Fatalf("content does not contain %q;\ncontent:\n%s", substr, content)
	}
}

func assertNotContains(t *testing.T, content, substr string) {
	t.Helper()
	if strings.Contains(content, substr) {
		t.Fatalf("content unexpectedly contains %q;\ncontent:\n%s", substr, content)
	}
}

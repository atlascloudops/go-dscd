package infrastructure

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	managedHeader = "# Managed by dscd — do not edit"
	shellDirRel   = ".config/dsc/shell"

	// Hook markers used for idempotent grep-guarding.
	bashHookMarker = "# dsc-env-hook"
	zshHookMarker  = "# dsc-env-hook"

	envShFilename   = "env.sh"
	envFishFilename = "env.fish"

	dirPerm  os.FileMode = 0755
	filePerm os.FileMode = 0644
)

// ShellAdapter defines the contract for shell-specific environment injection.
type ShellAdapter interface {
	// Install creates the sourcing hook for this shell (idempotent).
	Install(owner string) error
	// WriteEnv writes environment variables in this shell's native format.
	WriteEnv(owner string, env map[string]string) error
}

// ShellConfigurator coordinates environment injection across multiple shells.
type ShellConfigurator struct {
	adapters []ShellAdapter
	// homeDir overrides the base home directory (default: /home).
	// Exposed for testing; production callers use NewShellConfigurator.
	homeDir string
}

// NewShellConfigurator returns a ShellConfigurator with the default set of
// shell adapters: Bash, Zsh, and Fish.
func NewShellConfigurator() *ShellConfigurator {
	return &ShellConfigurator{
		adapters: []ShellAdapter{
			&BashAdapter{},
			&ZshAdapter{},
			&FishAdapter{},
		},
	}
}

// resolveHome returns the home directory for the given owner.
func (s *ShellConfigurator) resolveHome(owner string) string {
	base := "/home"
	if s.homeDir != "" {
		base = s.homeDir
	}
	return filepath.Join(base, owner)
}

// resolveShellDir returns the managed shell config directory for the given owner.
func (s *ShellConfigurator) resolveShellDir(owner string) string {
	return filepath.Join(s.resolveHome(owner), shellDirRel)
}

// Install creates the managed shell directory and installs sourcing hooks for
// all configured shell adapters.
func (s *ShellConfigurator) Install(owner string) error {
	shellDir := s.resolveShellDir(owner)
	if err := os.MkdirAll(shellDir, dirPerm); err != nil {
		return fmt.Errorf("create shell directory: %w", err)
	}
	chownBestEffort(shellDir, owner)

	for _, a := range s.adapters {
		if err := a.Install(owner); err != nil {
			return fmt.Errorf("install shell adapter: %w", err)
		}
	}
	return nil
}

// SetEnvironment merges the given environment variables into the managed env
// files for all shell adapters. Existing keys are updated, new keys are
// appended, and unmentioned keys are preserved.
func (s *ShellConfigurator) SetEnvironment(owner string, env map[string]string) error {
	shellDir := s.resolveShellDir(owner)
	if err := os.MkdirAll(shellDir, dirPerm); err != nil {
		return fmt.Errorf("create shell directory: %w", err)
	}

	// Merge into env.sh (POSIX — shared by bash and zsh)
	shPath := filepath.Join(shellDir, envShFilename)
	if err := mergeEnvFile(shPath, env, formatPosixExport); err != nil {
		return fmt.Errorf("write env.sh: %w", err)
	}
	chownBestEffort(shPath, owner)

	// Merge into env.fish
	fishPath := filepath.Join(shellDir, envFishFilename)
	if err := mergeEnvFile(fishPath, env, formatFishExport); err != nil {
		return fmt.Errorf("write env.fish: %w", err)
	}
	chownBestEffort(fishPath, owner)

	return nil
}

// UnsetEnvironment removes the specified keys from the managed env files for
// all shell adapters.
func (s *ShellConfigurator) UnsetEnvironment(owner string, keys []string) error {
	shellDir := s.resolveShellDir(owner)

	shPath := filepath.Join(shellDir, envShFilename)
	if err := removeEnvKeys(shPath, keys, parsePosixKey); err != nil {
		return fmt.Errorf("unset env.sh: %w", err)
	}

	fishPath := filepath.Join(shellDir, envFishFilename)
	if err := removeEnvKeys(fishPath, keys, parseFishKey); err != nil {
		return fmt.Errorf("unset env.fish: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Shell adapters
// ---------------------------------------------------------------------------

// BashAdapter installs a sourcing hook in ~/.bashrc and writes POSIX env vars.
type BashAdapter struct {
	homeDir string // override for testing; empty uses /home
}

func (a *BashAdapter) Install(owner string) error {
	home := resolveHomePath(a.homeDir, owner)
	shellDir := filepath.Join(home, shellDirRel)
	envFile := filepath.Join(shellDir, envShFilename)
	hookLine := fmt.Sprintf("[ -f %q ] && . %q %s", envFile, envFile, bashHookMarker)
	rcPath := filepath.Join(home, ".bashrc")
	return appendLineIdempotent(rcPath, bashHookMarker, hookLine, owner)
}

func (a *BashAdapter) WriteEnv(owner string, env map[string]string) error {
	// Writing is handled by ShellConfigurator.SetEnvironment to avoid
	// double-writing env.sh. This is a no-op.
	return nil
}

// ZshAdapter installs a sourcing hook in ~/.zshenv. It shares env.sh with
// BashAdapter since the format is POSIX-compatible.
type ZshAdapter struct {
	homeDir string // override for testing; empty uses /home
}

func (a *ZshAdapter) Install(owner string) error {
	home := resolveHomePath(a.homeDir, owner)
	shellDir := filepath.Join(home, shellDirRel)
	envFile := filepath.Join(shellDir, envShFilename)
	hookLine := fmt.Sprintf("[ -f %q ] && . %q %s", envFile, envFile, zshHookMarker)
	rcPath := filepath.Join(home, ".zshenv")
	return appendLineIdempotent(rcPath, zshHookMarker, hookLine, owner)
}

func (a *ZshAdapter) WriteEnv(owner string, env map[string]string) error {
	// Shares env.sh with BashAdapter — no-op.
	return nil
}

// FishAdapter installs an autoload config file and writes fish-native env vars.
type FishAdapter struct {
	homeDir string // override for testing; empty uses /home
}

func (a *FishAdapter) Install(owner string) error {
	home := resolveHomePath(a.homeDir, owner)
	shellDir := filepath.Join(home, shellDirRel)
	envFile := filepath.Join(shellDir, envFishFilename)
	confDir := filepath.Join(home, ".config", "fish", "conf.d")
	if err := os.MkdirAll(confDir, dirPerm); err != nil {
		return fmt.Errorf("create fish conf.d: %w", err)
	}
	chownBestEffort(confDir, owner)

	autoloadPath := filepath.Join(confDir, "dsc-env.fish")
	content := fmt.Sprintf("%s\nif test -f %q\n    source %q\nend\n", managedHeader, envFile, envFile)
	return atomicWrite(autoloadPath, content, filePerm, owner)
}

func (a *FishAdapter) WriteEnv(owner string, env map[string]string) error {
	// Writing is handled by ShellConfigurator.SetEnvironment to avoid
	// double-writing env.fish. This is a no-op.
	return nil
}

// resolveHomePath returns the home directory for an owner, using baseDir if
// set, otherwise defaulting to /home.
func resolveHomePath(baseDir, owner string) string {
	base := "/home"
	if baseDir != "" {
		base = baseDir
	}
	return filepath.Join(base, owner)
}

// ---------------------------------------------------------------------------
// Format helpers
// ---------------------------------------------------------------------------

// formatPosixExport formats a key/value pair as a POSIX export line.
func formatPosixExport(key, value string) string {
	return fmt.Sprintf("export %s=%q", key, value)
}

// formatFishExport formats a key/value pair as a fish set -gx line.
func formatFishExport(key, value string) string {
	return fmt.Sprintf("set -gx %s %q", key, value)
}

// parsePosixKey extracts the key name from a POSIX export line.
// Returns ("", false) for non-export lines.
func parsePosixKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "export ") {
		return "", false
	}
	rest := strings.TrimPrefix(trimmed, "export ")
	eqIdx := strings.Index(rest, "=")
	if eqIdx < 1 {
		return "", false
	}
	return rest[:eqIdx], true
}

// parseFishKey extracts the key name from a fish set -gx line.
// Returns ("", false) for non-set lines.
func parseFishKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "set -gx ") {
		return "", false
	}
	rest := strings.TrimPrefix(trimmed, "set -gx ")
	fields := strings.Fields(rest)
	if len(fields) < 1 {
		return "", false
	}
	return fields[0], true
}

// ---------------------------------------------------------------------------
// File operations
// ---------------------------------------------------------------------------

// mergeEnvFile reads an existing env file, merges the new entries (update
// existing, append new, preserve unmentioned), and atomically writes back.
func mergeEnvFile(path string, env map[string]string, formatter func(string, string) string) error {
	// Determine the key parser based on the formatter.
	var keyParser func(string) (string, bool)
	// We detect fish vs posix by checking the formatter output.
	probe := formatter("_", "_")
	if strings.HasPrefix(probe, "set ") {
		keyParser = parseFishKey
	} else {
		keyParser = parsePosixKey
	}

	existing, err := readLines(path)
	if err != nil {
		return err
	}

	// Track which keys from env were already present.
	merged := make(map[string]bool)
	var result []string

	for _, line := range existing {
		key, ok := keyParser(line)
		if ok {
			if newVal, found := env[key]; found {
				// Update existing key.
				result = append(result, formatter(key, newVal))
				merged[key] = true
				continue
			}
		}
		// Preserve unmentioned keys and non-export lines.
		result = append(result, line)
	}

	// Append new keys not yet in file (sorted for deterministic output).
	var newKeys []string
	for k := range env {
		if !merged[k] {
			newKeys = append(newKeys, k)
		}
	}
	sort.Strings(newKeys)
	for _, k := range newKeys {
		result = append(result, formatter(k, env[k]))
	}

	// Ensure header is present.
	if len(result) == 0 || result[0] != managedHeader {
		result = append([]string{managedHeader}, result...)
	}

	content := strings.Join(result, "\n") + "\n"
	return atomicWrite(path, content, filePerm, "")
}

// removeEnvKeys removes lines matching the given keys from an env file.
func removeEnvKeys(path string, keys []string, keyParser func(string) (string, bool)) error {
	existing, err := readLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	removeSet := make(map[string]bool, len(keys))
	for _, k := range keys {
		removeSet[k] = true
	}

	var result []string
	for _, line := range existing {
		key, ok := keyParser(line)
		if ok && removeSet[key] {
			continue
		}
		result = append(result, line)
	}

	if len(result) == 0 {
		result = []string{managedHeader}
	}

	content := strings.Join(result, "\n") + "\n"
	return atomicWrite(path, content, filePerm, "")
}

// readLines reads all lines from a file. Returns nil, nil if the file does not
// exist.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// appendLineIdempotent appends a line to a file only if a marker string is
// not already present. Creates the file if it does not exist.
func appendLineIdempotent(path, marker, line, owner string) error {
	existing, err := readLines(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	// Check if marker already present.
	for _, l := range existing {
		if strings.Contains(l, marker) {
			return nil // already installed
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("open %s for append: %w", path, err)
	}
	defer f.Close()

	// Ensure newline before our hook if file is non-empty.
	if len(existing) > 0 {
		last := existing[len(existing)-1]
		if last != "" {
			fmt.Fprintln(f)
		}
	}

	_, err = fmt.Fprintln(f, line)
	if err != nil {
		return fmt.Errorf("write hook to %s: %w", path, err)
	}

	chownBestEffort(path, owner)
	return nil
}

// atomicWrite writes content to a file atomically via a temp file + rename.
func atomicWrite(path, content string, perm os.FileMode, owner string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".dsc-env-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("chmod: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}

	if owner != "" {
		chownBestEffort(path, owner)
	}
	return nil
}

// chownBestEffort sets ownership of a path to the given user (best-effort).
func chownBestEffort(path, owner string) {
	if owner == "" {
		return
	}
	_ = exec.Command("chown", owner+":"+owner, path).Run()
}

// shellDirPath returns the managed shell config directory for a user.
func shellDirPath(owner string) string {
	return filepath.Join("/home", owner, shellDirRel)
}

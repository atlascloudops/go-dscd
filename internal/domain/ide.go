package domain

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IDEContext carries the information needed to start an IDE instance for a worktree.
type IDEContext struct {
	Owner        string
	WorktreePath string
	WorktreeName string
	Port         int
}

// IDEState is the serializable snapshot of an IDE adapter attached to a workspace.
type IDEState struct {
	AdapterName string `json:"adapter_name"`
	Port        int    `json:"port"`
	Active      bool   `json:"active"`
}

// IDEAdapter is the interface for managing an IDE process as an ephemeral
// runtime adapter attached to a workspace worktree.
type IDEAdapter interface {
	// Name returns the adapter identifier (e.g. "openvscode-server").
	Name() string
	// Start writes the environment file, starts the systemd unit, and polls
	// for readiness.
	Start(ctx IDEContext) error
	// Stop stops the systemd unit and removes the environment file.
	Stop(ctx IDEContext) error
	// HealthCheck returns nil when the IDE is healthy (HTTP 200 on localhost:port).
	HealthCheck(ctx IDEContext) error
}

// CodeServerAdapter manages an openvscode-server instance via systemd template units.
type CodeServerAdapter struct {
	// EnvDir is the directory for per-instance env files (default: /opt/dsc/var/dscd/ide/).
	EnvDir string
	// SystemdRunner abstracts systemctl invocations for testability.
	SystemdRunner SystemdRunner
	// HTTPChecker abstracts HTTP health checks for testability.
	HTTPChecker HTTPChecker
	// PollTimeout is how long Start() polls for readiness (default: 5s).
	PollTimeout time.Duration
	// PollInterval is the interval between readiness polls (default: 250ms).
	PollInterval time.Duration
}

// SystemdRunner abstracts systemctl commands so the adapter is testable without
// a real init system.
type SystemdRunner interface {
	Start(unit string) error
	Stop(unit string) error
	IsActive(unit string) (bool, error)
}

// HTTPChecker abstracts an HTTP GET check for testability.
type HTTPChecker interface {
	Check(url string) error
}

// defaultSystemdRunner shells out to systemctl.
type defaultSystemdRunner struct{}

func (d *defaultSystemdRunner) Start(unit string) error {
	return exec.Command("systemctl", "start", unit).Run()
}

func (d *defaultSystemdRunner) Stop(unit string) error {
	return exec.Command("systemctl", "stop", unit).Run()
}

func (d *defaultSystemdRunner) IsActive(unit string) (bool, error) {
	err := exec.Command("systemctl", "is-active", "--quiet", unit).Run()
	if err == nil {
		return true, nil
	}
	// Exit code 3 means inactive — not an error
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 3 {
			return false, nil
		}
	}
	return false, err
}

// defaultHTTPChecker performs a real HTTP GET.
type defaultHTTPChecker struct{}

func (d *defaultHTTPChecker) Check(url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// NewCodeServerAdapter creates a CodeServerAdapter with production defaults.
func NewCodeServerAdapter() *CodeServerAdapter {
	return &CodeServerAdapter{
		EnvDir:        "/opt/dsc/var/dscd/ide/",
		SystemdRunner: &defaultSystemdRunner{},
		HTTPChecker:   &defaultHTTPChecker{},
		PollTimeout:   5 * time.Second,
		PollInterval:  250 * time.Millisecond,
	}
}

// Name returns the adapter identifier.
func (a *CodeServerAdapter) Name() string {
	return "openvscode-server"
}

// UnitName derives the systemd unit name deterministically from IDEContext fields.
func UnitName(ctx IDEContext) string {
	return fmt.Sprintf("openvscode-server@%s--%s.service", ctx.Owner, ctx.WorktreeName)
}

// envFilePath returns the path to the per-instance environment file.
func (a *CodeServerAdapter) envFilePath(ctx IDEContext) string {
	return filepath.Join(a.EnvDir, fmt.Sprintf("%s--%s.env", ctx.Owner, ctx.WorktreeName))
}

// Start writes the environment file, starts the systemd unit, and polls for
// readiness on localhost:port for up to PollTimeout.
func (a *CodeServerAdapter) Start(ctx IDEContext) error {
	// Ensure env dir exists
	if err := os.MkdirAll(a.EnvDir, 0755); err != nil {
		return fmt.Errorf("create env dir: %w", err)
	}

	// Write environment file
	envContent := fmt.Sprintf("IDE_OWNER=%s\nIDE_PORT=%d\nIDE_WORKTREE_PATH=%s\n",
		ctx.Owner, ctx.Port, ctx.WorktreePath)
	envPath := a.envFilePath(ctx)
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	// Start systemd unit
	unit := UnitName(ctx)
	if err := a.SystemdRunner.Start(unit); err != nil {
		return fmt.Errorf("start unit %s: %w", unit, err)
	}

	// Poll for readiness
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/", ctx.Port)
	deadline := time.Now().Add(a.PollTimeout)
	for time.Now().Before(deadline) {
		if err := a.HTTPChecker.Check(healthURL); err == nil {
			return nil
		}
		time.Sleep(a.PollInterval)
	}

	return fmt.Errorf("IDE did not become ready within %s", a.PollTimeout)
}

// Stop stops the systemd unit and removes the environment file.
func (a *CodeServerAdapter) Stop(ctx IDEContext) error {
	unit := UnitName(ctx)
	if err := a.SystemdRunner.Stop(unit); err != nil {
		return fmt.Errorf("stop unit %s: %w", unit, err)
	}

	envPath := a.envFilePath(ctx)
	if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove env file: %w", err)
	}
	return nil
}

// HealthCheck returns nil when the IDE at localhost:port responds HTTP 200.
func (a *CodeServerAdapter) HealthCheck(ctx IDEContext) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/", ctx.Port)
	return a.HTTPChecker.Check(url)
}

// envFileContent builds the env file body — exported for testing.
func envFileContent(ctx IDEContext) string {
	return fmt.Sprintf("IDE_OWNER=%s\nIDE_PORT=%d\nIDE_WORKTREE_PATH=%s\n",
		ctx.Owner, ctx.Port, ctx.WorktreePath)
}

// parseEnvFile reads key=value pairs from an env file (for diagnostics).
func parseEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result, nil
}

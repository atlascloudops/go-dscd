package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
)

// seedActivityLog writes test events to an activity log file and returns the path.
func seedActivityLog(t *testing.T, events []domain.EventRecord) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := domain.NewActivityLog(path)
	for _, e := range events {
		if err := al.Append(e); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return path
}

// executeEventsCmd runs the events command with the given args and captures stdout.
// It does NOT reset the jsonOutput global — callers must manage that flag.
func executeEventsCmd(t *testing.T, path string, args ...string) (string, error) {
	t.Helper()

	al := domain.NewActivityLog(path)
	cmd := newEventsCmd(func() *domain.ActivityLog { return al }, &path)

	// Capture stdout.
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)

	// Redirect os.Stdout for fmt.Print* calls.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd.SetArgs(args)
	err := cmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	// Read captured output.
	pipeOut := make([]byte, 0)
	pipeBuf := make([]byte, 4096)
	for {
		n, readErr := r.Read(pipeBuf)
		if n > 0 {
			pipeOut = append(pipeOut, pipeBuf[:n]...)
		}
		if readErr != nil {
			break
		}
	}
	r.Close()

	output := string(pipeOut) + buf.String()
	return output, err
}

func testEvents() []domain.EventRecord {
	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	return []domain.EventRecord{
		{Scope: domain.EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_started", Timestamp: ts, Detail: "github.com/org/infra"},
		{Scope: domain.EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_completed", Timestamp: ts.Add(10 * time.Second)},
		{Scope: domain.EventScope{Kind: "workspace", Name: "infra"}, Event: "worktree_created", Timestamp: ts.Add(11 * time.Second), Detail: "branch=main"},
		{Scope: domain.EventScope{Kind: "ide", Name: "infra"}, Event: "ide_started", Timestamp: ts.Add(12 * time.Second), Detail: "port=9100"},
		{Scope: domain.EventScope{Kind: "ide", Name: "infra"}, Event: "ide_ready", Timestamp: ts.Add(14 * time.Second), Detail: "port=9100"},
		{Scope: domain.EventScope{Kind: "credentials", Name: "jperez"}, Event: "git_credentials_written", Timestamp: ts.Add(8 * time.Minute), Detail: "hosts=github.com added=1"},
	}
}

func TestEvents_NoFilter_ReturnsAll(t *testing.T) {
	jsonOutput = false
	events := testEvents()
	path := seedActivityLog(t, events)

	output, err := executeEventsCmd(t, path, "--lines", "100")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Should contain all 6 events plus header.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Header + 6 data rows.
	if len(lines) != 7 {
		t.Errorf("expected 7 lines (header + 6 events), got %d:\n%s", len(lines), output)
	}

	// Verify header columns exist.
	if !strings.Contains(lines[0], "TIMESTAMP") || !strings.Contains(lines[0], "SCOPE") {
		t.Errorf("expected header with TIMESTAMP and SCOPE, got: %s", lines[0])
	}
}

func TestEvents_KindFilter(t *testing.T) {
	jsonOutput = false
	events := testEvents()
	path := seedActivityLog(t, events)

	output, err := executeEventsCmd(t, path, "--kind", "workspace", "--lines", "100")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Header + 3 workspace events.
	if len(lines) != 4 {
		t.Errorf("expected 4 lines (header + 3 workspace events), got %d:\n%s", len(lines), output)
	}
	for _, line := range lines[1:] {
		if !strings.Contains(line, "workspace:") {
			t.Errorf("expected workspace scope, got: %s", line)
		}
	}
}

func TestEvents_ScopeFilter(t *testing.T) {
	jsonOutput = false
	events := testEvents()
	path := seedActivityLog(t, events)

	output, err := executeEventsCmd(t, path, "--scope", "ide:infra", "--lines", "100")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Header + 2 ide:infra events.
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 ide:infra events), got %d:\n%s", len(lines), output)
	}
	for _, line := range lines[1:] {
		if !strings.Contains(line, "ide:infra") {
			t.Errorf("expected ide:infra scope, got: %s", line)
		}
	}
}

func TestEvents_SinceFilter(t *testing.T) {
	jsonOutput = false
	// Create events spanning 10 minutes.
	ts := time.Now().UTC()
	events := []domain.EventRecord{
		{Scope: domain.EventScope{Kind: "workspace", Name: "infra"}, Event: "old_event", Timestamp: ts.Add(-2 * time.Hour)},
		{Scope: domain.EventScope{Kind: "workspace", Name: "infra"}, Event: "recent_event_1", Timestamp: ts.Add(-30 * time.Minute)},
		{Scope: domain.EventScope{Kind: "workspace", Name: "infra"}, Event: "recent_event_2", Timestamp: ts.Add(-10 * time.Minute)},
	}
	path := seedActivityLog(t, events)

	output, err := executeEventsCmd(t, path, "--since", "1h", "--lines", "100")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Header + 2 recent events (old_event is >1h ago).
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 recent events), got %d:\n%s", len(lines), output)
	}
	if strings.Contains(output, "old_event") {
		t.Error("expected old_event to be filtered out by --since 1h")
	}
}

func TestEvents_JSONOutput(t *testing.T) {
	events := testEvents()
	path := seedActivityLog(t, events)

	// Set jsonOutput directly since --json is a persistent flag on the root command.
	jsonOutput = true
	defer func() { jsonOutput = false }()

	output, err := executeEventsCmd(t, path, "--lines", "100")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Should be valid JSON array.
	var records []domain.EventRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &records); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, output)
	}

	if len(records) != 6 {
		t.Errorf("expected 6 records in JSON, got %d", len(records))
	}
}

func TestEvents_EmptyLog_NoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")

	output, err := executeEventsCmd(t, path, "--lines", "100")
	if err != nil {
		t.Fatalf("execute: %v (should not error on empty log)", err)
	}
	if !strings.Contains(output, "No events found") {
		t.Errorf("expected 'No events found' message, got: %s", output)
	}
}

func TestEvents_EmptyLog_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")

	// Set jsonOutput directly since --json is a persistent flag on the root command.
	jsonOutput = true
	defer func() { jsonOutput = false }()

	output, err := executeEventsCmd(t, path)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var records []domain.EventRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &records); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty JSON array, got %d records", len(records))
	}
}

func TestEvents_LinesLimit(t *testing.T) {
	events := testEvents() // 6 events
	path := seedActivityLog(t, events)

	output, err := executeEventsCmd(t, path, "--lines", "3")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	// Header + 3 most recent events.
	if len(lines) != 4 {
		t.Errorf("expected 4 lines (header + 3 events), got %d:\n%s", len(lines), output)
	}

	// The last 3 events should be: ide_ready, git_credentials_written, and one more.
	if !strings.Contains(output, "git_credentials_written") {
		t.Error("expected last event git_credentials_written to be present")
	}
}

func TestEvents_ScopeAndKindMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")

	_, err := executeEventsCmd(t, path, "--scope", "ide:infra", "--kind", "workspace")
	if err == nil {
		t.Fatal("expected error when using --scope and --kind together")
	}
}

func TestEvents_InvalidScopeFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")

	_, err := executeEventsCmd(t, path, "--scope", "invalid-scope")
	if err == nil {
		t.Fatal("expected error for invalid scope format")
	}
}

func TestEvents_InvalidSinceDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")

	_, err := executeEventsCmd(t, path, "--since", "not-a-duration")
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

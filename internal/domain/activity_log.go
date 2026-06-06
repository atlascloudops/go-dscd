package domain

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// DefaultActivityLogPath is the default path for the unified activity log file.
// Aligned with the existing application root until the layout story lands.
const DefaultActivityLogPath = "/opt/dsc/var/dscd/activity.log"

// ActivityLogFilter controls which events are returned by ActivityLog.Read.
// All fields are optional — zero values mean "no filter".
type ActivityLogFilter struct {
	ScopeKind string    // filter by scope kind (e.g. "workspace")
	ScopeName string    // filter by scope name (e.g. "infra")
	Since     time.Time // exclude events before this timestamp
}

// ActivityLog is a concurrent-safe, append-only file writer for EventRecord
// entries. It provides a unified chronological view of domain events across
// all aggregates.
type ActivityLog struct {
	path string
	mu   sync.Mutex
}

// NewActivityLog creates an ActivityLog that writes to the given file path.
func NewActivityLog(path string) *ActivityLog {
	return &ActivityLog{path: path}
}

// Append writes a single EventRecord as a human-readable line to the activity
// log file. The file is created if it does not exist. Concurrent calls are
// serialized via a mutex to prevent interleaved writes.
//
// Line format:
//
//	[2024-06-06T14:02:20Z] [workspace:infra] clone_started	detail-text-here
func (a *ActivityLog) Append(record EventRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("activity log: open %s: %w", a.path, err)
	}
	defer f.Close()

	line := formatLine(record)
	if _, err := f.WriteString(line + "\n"); err != nil {
		return fmt.Errorf("activity log: write: %w", err)
	}
	return nil
}

// Read parses the activity log file and returns EventRecord entries matching
// the given filter. If the file does not exist, it returns an empty slice.
func (a *ActivityLog) Read(filter ActivityLogFilter) ([]EventRecord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.Open(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("activity log: open %s: %w", a.path, err)
	}
	defer f.Close()

	var records []EventRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		record, err := parseLine(line)
		if err != nil {
			// Skip malformed lines rather than failing the entire read.
			continue
		}
		if !matchesFilter(record, filter) {
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("activity log: scan: %w", err)
	}
	return records, nil
}

// formatLine renders an EventRecord as a single log line.
// Format: [<RFC3339-UTC>] [<scope>] <event>\t<detail>
// Detail and its preceding tab are omitted when empty.
func formatLine(r EventRecord) string {
	ts := r.Timestamp.UTC().Format(time.RFC3339)
	base := fmt.Sprintf("[%s] [%s] %s", ts, r.Scope.String(), r.Event)
	if r.Detail != "" {
		return base + "\t" + r.Detail
	}
	return base
}

// parseLine reconstructs an EventRecord from a formatted log line.
func parseLine(line string) (EventRecord, error) {
	// Expected format: [<timestamp>] [<scope>] <event>\t<detail>
	// Minimum: [2024-06-06T14:02:20Z] [workspace:infra] clone_started

	if len(line) < 2 || line[0] != '[' {
		return EventRecord{}, fmt.Errorf("invalid line: missing timestamp bracket")
	}

	// Extract timestamp between first pair of brackets.
	tsEnd := strings.Index(line, "]")
	if tsEnd < 0 {
		return EventRecord{}, fmt.Errorf("invalid line: unclosed timestamp bracket")
	}
	tsStr := line[1:tsEnd]
	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		return EventRecord{}, fmt.Errorf("invalid timestamp %q: %w", tsStr, err)
	}

	// After "] " expect "[scope] event..."
	rest := line[tsEnd+1:]
	rest = strings.TrimLeft(rest, " ")

	if len(rest) < 2 || rest[0] != '[' {
		return EventRecord{}, fmt.Errorf("invalid line: missing scope bracket")
	}
	scopeEnd := strings.Index(rest, "]")
	if scopeEnd < 0 {
		return EventRecord{}, fmt.Errorf("invalid line: unclosed scope bracket")
	}
	scopeStr := rest[1:scopeEnd]
	parts := strings.SplitN(scopeStr, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return EventRecord{}, fmt.Errorf("invalid scope %q", scopeStr)
	}

	// After "] " expect "event" optionally followed by "\tdetail"
	after := rest[scopeEnd+1:]
	after = strings.TrimLeft(after, " ")

	var event, detail string
	if tabIdx := strings.Index(after, "\t"); tabIdx >= 0 {
		event = after[:tabIdx]
		detail = after[tabIdx+1:]
	} else {
		event = strings.TrimRight(after, "\n\r")
	}

	if event == "" {
		return EventRecord{}, fmt.Errorf("invalid line: missing event name")
	}

	return EventRecord{
		Scope:     EventScope{Kind: parts[0], Name: parts[1]},
		Event:     event,
		Timestamp: ts,
		Detail:    detail,
	}, nil
}

// matchesFilter returns true if the record satisfies all non-zero filter fields.
func matchesFilter(r EventRecord, f ActivityLogFilter) bool {
	if f.ScopeKind != "" && r.Scope.Kind != f.ScopeKind {
		return false
	}
	if f.ScopeName != "" && r.Scope.Name != f.ScopeName {
		return false
	}
	if !f.Since.IsZero() && r.Timestamp.Before(f.Since) {
		return false
	}
	return true
}

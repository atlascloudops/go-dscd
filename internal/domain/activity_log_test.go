package domain

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActivityLog_Append_WritesFormattedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 2, 20, 0, time.UTC)
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra"},
		Event:     "clone_started",
		Timestamp: ts,
		Detail:    "starting bare clone",
	}

	if err := al.Append(record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	expected := "[2024-06-06T14:02:20Z] [workspace:infra] clone_started\tstarting bare clone\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
}

func TestActivityLog_Append_OmitsDetailWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 2, 20, 0, time.UTC)
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra"},
		Event:     "clone_completed",
		Timestamp: ts,
	}

	if err := al.Append(record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	expected := "[2024-06-06T14:02:20Z] [workspace:infra] clone_completed\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}
}

func TestActivityLog_Append_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "activity.log")

	// Create parent dir so OpenFile works
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	al := NewActivityLog(path)
	record := EventRecord{
		Scope:     EventScope{Kind: "ide", Name: "infra"},
		Event:     "ide_started",
		Timestamp: time.Now().UTC(),
	}

	if err := al.Append(record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to be created: %v", err)
	}
}

func TestActivityLog_Append_MultipleLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		record := EventRecord{
			Scope:     EventScope{Kind: "workspace", Name: "infra"},
			Event:     "clone_started",
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
		}
		if err := al.Append(record); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestActivityLog_Read_NoFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_started", Timestamp: ts},
		{Scope: EventScope{Kind: "ide", Name: "infra"}, Event: "ide_started", Timestamp: ts.Add(time.Minute)},
		{Scope: EventScope{Kind: "credentials", Name: "jperez"}, Event: "sso_synced", Timestamp: ts.Add(2 * time.Minute)},
	}
	for _, e := range events {
		if err := al.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	records, err := al.Read(ActivityLogFilter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("expected 3 records, got %d", len(records))
	}
}

func TestActivityLog_Read_FilterByScopeKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_started", Timestamp: ts},
		{Scope: EventScope{Kind: "ide", Name: "infra"}, Event: "ide_started", Timestamp: ts.Add(time.Minute)},
		{Scope: EventScope{Kind: "workspace", Name: "app"}, Event: "clone_started", Timestamp: ts.Add(2 * time.Minute)},
	}
	for _, e := range events {
		if err := al.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	records, err := al.Read(ActivityLogFilter{ScopeKind: "workspace"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 workspace records, got %d", len(records))
	}
	for _, r := range records {
		if r.Scope.Kind != "workspace" {
			t.Errorf("expected scope kind %q, got %q", "workspace", r.Scope.Kind)
		}
	}
}

func TestActivityLog_Read_FilterByScopeName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_started", Timestamp: ts},
		{Scope: EventScope{Kind: "workspace", Name: "app"}, Event: "clone_started", Timestamp: ts.Add(time.Minute)},
		{Scope: EventScope{Kind: "ide", Name: "infra"}, Event: "ide_started", Timestamp: ts.Add(2 * time.Minute)},
	}
	for _, e := range events {
		if err := al.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	records, err := al.Read(ActivityLogFilter{ScopeName: "infra"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records with name 'infra', got %d", len(records))
	}
}

func TestActivityLog_Read_FilterBySince(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_started", Timestamp: ts},
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_completed", Timestamp: ts.Add(5 * time.Minute)},
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "worktree_created", Timestamp: ts.Add(10 * time.Minute)},
	}
	for _, e := range events {
		if err := al.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	since := ts.Add(3 * time.Minute)
	records, err := al.Read(ActivityLogFilter{Since: since})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 records after %v, got %d", since, len(records))
	}
	for _, r := range records {
		if r.Timestamp.Before(since) {
			t.Errorf("record timestamp %v is before since %v", r.Timestamp, since)
		}
	}
}

func TestActivityLog_Read_CombinedFilters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	events := []EventRecord{
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_started", Timestamp: ts},
		{Scope: EventScope{Kind: "ide", Name: "infra"}, Event: "ide_started", Timestamp: ts.Add(time.Minute)},
		{Scope: EventScope{Kind: "workspace", Name: "infra"}, Event: "clone_completed", Timestamp: ts.Add(5 * time.Minute)},
		{Scope: EventScope{Kind: "workspace", Name: "app"}, Event: "clone_started", Timestamp: ts.Add(6 * time.Minute)},
	}
	for _, e := range events {
		if err := al.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	records, err := al.Read(ActivityLogFilter{
		ScopeKind: "workspace",
		ScopeName: "infra",
		Since:     ts.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Event != "clone_completed" {
		t.Errorf("expected event %q, got %q", "clone_completed", records[0].Event)
	}
}

func TestActivityLog_Read_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.log")
	al := NewActivityLog(path)

	records, err := al.Read(ActivityLogFilter{})
	if err != nil {
		t.Fatalf("Read: expected nil error for non-existent file, got %v", err)
	}
	if records != nil {
		t.Errorf("expected nil records, got %v", records)
	}
}

func TestActivityLog_Read_DetailPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra"},
		Event:     "clone_started",
		Timestamp: ts,
		Detail:    "starting bare clone of repo",
	}
	if err := al.Append(record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	records, err := al.Read(ActivityLogFilter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Detail != "starting bare clone of repo" {
		t.Errorf("expected detail %q, got %q", "starting bare clone of repo", records[0].Detail)
	}
}

func TestActivityLog_ConcurrentAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			record := EventRecord{
				Scope:     EventScope{Kind: "workspace", Name: "infra"},
				Event:     "clone_started",
				Timestamp: ts.Add(time.Duration(idx) * time.Second),
				Detail:    "concurrent write",
			}
			if err := al.Append(record); err != nil {
				t.Errorf("Append %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Verify all lines are valid and non-interleaved.
	records, err := al.Read(ActivityLogFilter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != n {
		t.Errorf("expected %d records, got %d", n, len(records))
	}
}

func TestActivityLog_Read_SlashInScopeName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "activity.log")
	al := NewActivityLog(path)

	ts := time.Date(2024, 6, 6, 14, 0, 0, 0, time.UTC)
	record := EventRecord{
		Scope:     EventScope{Kind: "workspace", Name: "infra/feat"},
		Event:     "clone_started",
		Timestamp: ts,
	}
	if err := al.Append(record); err != nil {
		t.Fatalf("Append: %v", err)
	}

	records, err := al.Read(ActivityLogFilter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Scope.Name != "infra/feat" {
		t.Errorf("expected scope name %q, got %q", "infra/feat", records[0].Scope.Name)
	}
}

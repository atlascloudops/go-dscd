package infrastructure

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPodOwner_LinuxUsername(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pod-config.json")
	content := `{"pod": {"linux_username": "jperez", "owner": "jperez-fallback"}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	owner, err := ReadPodOwner(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "jperez" {
		t.Errorf("expected 'jperez', got %q", owner)
	}
}

func TestReadPodOwner_FallbackToOwner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pod-config.json")
	content := `{"pod": {"owner": "fallback-user"}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	owner, err := ReadPodOwner(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "fallback-user" {
		t.Errorf("expected 'fallback-user', got %q", owner)
	}
}

func TestReadPodOwner_EmptyFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pod-config.json")
	content := `{"pod": {}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	owner, err := ReadPodOwner(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "" {
		t.Errorf("expected empty string, got %q", owner)
	}
}

func TestReadPodOwner_MissingFile(t *testing.T) {
	owner, err := ReadPodOwner("/nonexistent/path/pod-config.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if owner != "" {
		t.Errorf("expected empty string, got %q", owner)
	}
}

func TestReadPodOwner_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pod-config.json")
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	owner, err := ReadPodOwner(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if owner != "" {
		t.Errorf("expected empty string, got %q", owner)
	}
}

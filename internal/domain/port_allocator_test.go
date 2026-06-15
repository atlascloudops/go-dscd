package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortAllocator_Allocate(t *testing.T) {
	dir := t.TempDir()
	pa := NewPortAllocator(filepath.Join(dir, "ports.json"))

	port, err := pa.Allocate("alice--default")
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if port != 9100 {
		t.Errorf("expected first allocation to be 9100, got %d", port)
	}
}

func TestPortAllocator_IdempotentReAllocate(t *testing.T) {
	dir := t.TempDir()
	pa := NewPortAllocator(filepath.Join(dir, "ports.json"))

	port1, err := pa.Allocate("alice--default")
	if err != nil {
		t.Fatalf("first allocate: %v", err)
	}

	port2, err := pa.Allocate("alice--default")
	if err != nil {
		t.Fatalf("second allocate: %v", err)
	}

	if port1 != port2 {
		t.Errorf("expected idempotent allocation: got %d then %d", port1, port2)
	}
}

func TestPortAllocator_MultipleKeys(t *testing.T) {
	dir := t.TempDir()
	pa := NewPortAllocator(filepath.Join(dir, "ports.json"))

	p1, _ := pa.Allocate("alice--default")
	p2, _ := pa.Allocate("alice--feature")
	p3, _ := pa.Allocate("bob--default")

	if p1 == p2 || p2 == p3 || p1 == p3 {
		t.Errorf("expected unique ports, got %d, %d, %d", p1, p2, p3)
	}
	if p1 != 9100 || p2 != 9101 || p3 != 9102 {
		t.Errorf("expected sequential ports 9100,9101,9102; got %d,%d,%d", p1, p2, p3)
	}
}

func TestPortAllocator_Release(t *testing.T) {
	dir := t.TempDir()
	pa := NewPortAllocator(filepath.Join(dir, "ports.json"))

	pa.Allocate("alice--default") // 9100
	pa.Allocate("alice--feature") // 9101

	if err := pa.Release("alice--default"); err != nil {
		t.Fatalf("release: %v", err)
	}

	// Next allocation should reuse the freed port
	port, err := pa.Allocate("bob--default")
	if err != nil {
		t.Fatalf("allocate after release: %v", err)
	}
	if port != 9100 {
		t.Errorf("expected freed port 9100 to be reused, got %d", port)
	}
}

func TestPortAllocator_ReleaseNonExistent(t *testing.T) {
	dir := t.TempDir()
	pa := NewPortAllocator(filepath.Join(dir, "ports.json"))

	// Releasing a key that was never allocated should not error
	if err := pa.Release("nobody--nothing"); err != nil {
		t.Errorf("expected no error releasing non-existent key, got %v", err)
	}
}

func TestPortAllocator_Exhaustion(t *testing.T) {
	dir := t.TempDir()
	pa := &PortAllocator{
		MinPort:  9100,
		MaxPort:  9102, // only 3 ports
		FilePath: filepath.Join(dir, "ports.json"),
	}

	for i := 0; i < 3; i++ {
		if _, err := pa.Allocate(PortKey("user", string(rune('a'+i)))); err != nil {
			t.Fatalf("allocate %d: %v", i, err)
		}
	}

	// 4th should fail
	_, err := pa.Allocate("user--extra")
	if err == nil {
		t.Fatal("expected exhaustion error, got nil")
	}
	if got := err.Error(); got != "port range 9100–9102 exhausted" {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestPortAllocator_Persistence(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "ports.json")

	pa1 := NewPortAllocator(fp)
	pa1.Allocate("alice--default")

	// Create a new allocator pointing to the same file
	pa2 := NewPortAllocator(fp)
	port, err := pa2.Allocate("alice--default")
	if err != nil {
		t.Fatalf("allocate from fresh instance: %v", err)
	}
	if port != 9100 {
		t.Errorf("expected persisted port 9100, got %d", port)
	}
}

func TestPortAllocator_Lookup(t *testing.T) {
	dir := t.TempDir()
	pa := NewPortAllocator(filepath.Join(dir, "ports.json"))

	pa.Allocate("alice--default")

	port, ok := pa.Lookup("alice--default")
	if !ok || port != 9100 {
		t.Errorf("expected (9100, true), got (%d, %v)", port, ok)
	}

	_, ok = pa.Lookup("nobody--nothing")
	if ok {
		t.Error("expected false for non-existent key")
	}
}

func TestPortAllocator_MissingFile(t *testing.T) {
	pa := NewPortAllocator("/tmp/nonexistent-dscd-test-ports.json")
	defer os.Remove("/tmp/nonexistent-dscd-test-ports.json")

	port, err := pa.Allocate("test--key")
	if err != nil {
		t.Fatalf("allocate with missing file: %v", err)
	}
	if port != 9100 {
		t.Errorf("expected 9100, got %d", port)
	}
}

func TestPortKey(t *testing.T) {
	got := PortKey("alice", "default")
	if got != "alice--default" {
		t.Errorf("expected alice--default, got %s", got)
	}
}

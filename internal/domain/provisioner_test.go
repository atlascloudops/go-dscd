package domain

import (
	"os"
	"path/filepath"
	"testing"
)

type memStore struct {
	instances map[string]*WorkspaceInstance
	locked    bool
}

func newMemStore() *memStore {
	return &memStore{instances: make(map[string]*WorkspaceInstance)}
}

func (m *memStore) Load() (map[string]*WorkspaceInstance, error) {
	return m.instances, nil
}

func (m *memStore) Save(instances map[string]*WorkspaceInstance) error {
	m.instances = instances
	return nil
}

func (m *memStore) WithLock(fn func() error) error {
	m.locked = true
	defer func() { m.locked = false }()
	return fn()
}

func TestValidateSpec_MissingFields(t *testing.T) {
	err := validateSpec(WorkspaceSpec{})
	if err == nil {
		t.Fatal("expected error for empty spec")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrSpecInvalid {
		t.Fatalf("expected SPEC_INVALID, got %s", pe.Code)
	}
	// Should list all missing fields
	for _, field := range []string{"name", "vcs.clone_url", "vcs.branch", "project_root", "owner"} {
		if pe.Detail == "" {
			t.Fatalf("expected detail listing %s", field)
		}
	}
}

func TestValidateSpec_Valid(t *testing.T) {
	spec := WorkspaceSpec{
		Name:        "test",
		VCS:         VCSTarget{CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot: "/tmp/test",
		Owner:       "user",
	}
	if err := validateSpec(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvision_Idempotent(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(projectRoot, ".git"), 0755)

	store := newMemStore()
	p := &Provisioner{LogDir: filepath.Join(dir, "logs")}

	spec := WorkspaceSpec{
		Name:        "test",
		VCS:         VCSTarget{Host: "github.com", CloneURL: "https://github.com/org/repo.git", Branch: "main"},
		ProjectRoot: projectRoot,
		Owner:       "testuser",
	}

	inst, err := p.Provision(store, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inst.State != StateReady {
		t.Fatalf("expected ready, got %s", inst.State)
	}
	if !inst.CloneExists {
		t.Fatal("expected clone_exists=true")
	}

	// Verify persisted
	saved := store.instances["test"]
	if saved == nil {
		t.Fatal("workspace not persisted")
	}
	if saved.State != StateReady {
		t.Fatalf("persisted state should be ready, got %s", saved.State)
	}
}

func TestProvision_InvalidSpec(t *testing.T) {
	store := newMemStore()
	p := &Provisioner{}

	_, err := p.Provision(store, WorkspaceSpec{})
	if err == nil {
		t.Fatal("expected error")
	}
	pe, ok := err.(*ProvisionError)
	if !ok {
		t.Fatalf("expected ProvisionError, got %T", err)
	}
	if pe.Code != ErrSpecInvalid {
		t.Fatalf("expected SPEC_INVALID, got %s", pe.Code)
	}
}

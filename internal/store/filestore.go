package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
)

type StateFile struct {
	Version     string                               `json:"version"`
	UpdatedAt   time.Time                            `json:"updated_at"`
	Workspaces  map[string]*domain.Workspace  `json:"workspaces"`
	Credentials map[string]*domain.CredentialState    `json:"credentials,omitempty"`
}

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load() (map[string]*domain.Workspace, error) {
	state, err := s.LoadState()
	if err != nil {
		return nil, err
	}
	return state.Workspaces, nil
}

func (s *FileStore) Save(instances map[string]*domain.Workspace) error {
	// Load existing state to preserve credentials across workspace-only saves
	existing, _ := s.loadRaw()
	var creds map[string]*domain.CredentialState
	if existing != nil {
		// Parse just the credentials from the raw state.
		var sf StateFile
		if err := json.Unmarshal(existing, &sf); err == nil {
			creds = sf.Credentials
		}
	}
	return s.SaveState(&domain.DaemonState{
		Workspaces:  instances,
		Credentials: creds,
	})
}

func (s *FileStore) LoadState() (*domain.DaemonState, error) {
	raw, err := s.loadRaw()
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return &domain.DaemonState{
			Workspaces:  make(map[string]*domain.Workspace),
			Credentials: make(map[string]*domain.CredentialState),
		}, nil
	}

	var sf *StateFile

	// Check if migration is needed by inspecting raw JSON.
	if needsMigrationRaw(raw) {
		migrated, migrateErr := migrateV1ToV2(raw)
		if migrateErr != nil {
			return nil, migrateErr
		}
		sf = migrated
	} else {
		var parsed StateFile
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		sf = &parsed
	}

	state := &domain.DaemonState{
		Workspaces:  sf.Workspaces,
		Credentials: sf.Credentials,
	}
	if state.Workspaces == nil {
		state.Workspaces = make(map[string]*domain.Workspace)
	}
	if state.Credentials == nil {
		state.Credentials = make(map[string]*domain.CredentialState)
	}
	return state, nil
}

func (s *FileStore) SaveState(state *domain.DaemonState) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	sf := StateFile{
		Version:     StateVersionV2,
		UpdatedAt:   time.Now().UTC(),
		Workspaces:  state.Workspaces,
		Credentials: state.Credentials,
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0664)
}

// loadRaw reads the state file as raw bytes, returning nil if the file does not exist.
func (s *FileStore) loadRaw() ([]byte, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// loadStateFile reads and parses the state file, returning nil if the file does not exist.
func (s *FileStore) loadStateFile() (*StateFile, error) {
	data, err := s.loadRaw()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	return &sf, nil
}

func (s *FileStore) Path() string {
	return s.path
}

func (s *FileStore) lockPath() string {
	return s.path + ".lock"
}

func (s *FileStore) WithLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.lockPath()), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.lockPath(), os.O_CREATE|os.O_RDWR, 0664)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

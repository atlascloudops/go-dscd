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
	Version    string                               `json:"version"`
	UpdatedAt  time.Time                            `json:"updated_at"`
	Workspaces map[string]*domain.WorkspaceInstance `json:"workspaces"`
}

type FileStore struct {
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Load() (map[string]*domain.WorkspaceInstance, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*domain.WorkspaceInstance), nil
		}
		return nil, err
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, err
	}
	if sf.Workspaces == nil {
		return make(map[string]*domain.WorkspaceInstance), nil
	}
	return sf.Workspaces, nil
}

func (s *FileStore) Save(instances map[string]*domain.WorkspaceInstance) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	sf := StateFile{
		Version:    "v1",
		UpdatedAt:  time.Now().UTC(),
		Workspaces: instances,
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0664)
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

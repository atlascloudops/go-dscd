package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PortAllocator manages unique port assignments from a configurable range,
// persisting them to a JSON file. Allocations are keyed by worktree identity
// (owner--worktree_name) and are idempotent — requesting the same key returns
// the same port.
type PortAllocator struct {
	// MinPort is the inclusive lower bound of the allocatable range.
	MinPort int
	// MaxPort is the inclusive upper bound of the allocatable range.
	MaxPort int
	// FilePath is where allocations are persisted (JSON).
	FilePath string
}

// portState is the serialized form of the allocator's assignment table.
type portState struct {
	Assignments map[string]int `json:"assignments"` // key -> port
}

// NewPortAllocator creates an allocator for the default range 9100–9199
// persisted at the given path.
func NewPortAllocator(filePath string) *PortAllocator {
	return &PortAllocator{
		MinPort:  9100,
		MaxPort:  9199,
		FilePath: filePath,
	}
}

// Allocate returns a port for the given key. If the key already has a port,
// the same port is returned (idempotent). Otherwise the lowest free port in
// the range is assigned and persisted.
func (pa *PortAllocator) Allocate(key string) (int, error) {
	state, err := pa.load()
	if err != nil {
		return 0, fmt.Errorf("load port state: %w", err)
	}

	// Idempotent: return existing assignment
	if port, ok := state.Assignments[key]; ok {
		return port, nil
	}

	// Build set of used ports
	used := make(map[int]bool, len(state.Assignments))
	for _, p := range state.Assignments {
		used[p] = true
	}

	// Find lowest free port
	for port := pa.MinPort; port <= pa.MaxPort; port++ {
		if !used[port] {
			state.Assignments[key] = port
			if err := pa.save(state); err != nil {
				return 0, fmt.Errorf("save port state: %w", err)
			}
			return port, nil
		}
	}

	return 0, fmt.Errorf("port range %d–%d exhausted", pa.MinPort, pa.MaxPort)
}

// Release frees the port assigned to key, making it available for reuse.
func (pa *PortAllocator) Release(key string) error {
	state, err := pa.load()
	if err != nil {
		return fmt.Errorf("load port state: %w", err)
	}

	if _, ok := state.Assignments[key]; !ok {
		return nil // nothing to release
	}

	delete(state.Assignments, key)
	return pa.save(state)
}

// Lookup returns the port assigned to key and true, or 0 and false if none.
func (pa *PortAllocator) Lookup(key string) (int, bool) {
	state, err := pa.load()
	if err != nil {
		return 0, false
	}
	port, ok := state.Assignments[key]
	return port, ok
}

// PortKey builds the canonical allocation key from owner and worktree name.
func PortKey(owner, worktreeName string) string {
	return fmt.Sprintf("%s--%s", owner, worktreeName)
}

func (pa *PortAllocator) load() (*portState, error) {
	data, err := os.ReadFile(pa.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &portState{Assignments: make(map[string]int)}, nil
		}
		return nil, err
	}
	var state portState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Assignments == nil {
		state.Assignments = make(map[string]int)
	}
	return &state, nil
}

func (pa *PortAllocator) save(state *portState) error {
	if err := os.MkdirAll(filepath.Dir(pa.FilePath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pa.FilePath, data, 0644)
}

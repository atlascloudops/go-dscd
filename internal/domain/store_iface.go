package domain

// DaemonState is the top-level state envelope that holds all aggregate state.
// It is the unit of persistence for the daemon's filestore.
type DaemonState struct {
	Workspaces  map[string]*Workspace `json:"workspaces"`
	Credentials map[string]*CredentialState   `json:"credentials,omitempty"`
}

// StateStore abstracts state persistence so domain logic doesn't depend on the store package.
type StateStore interface {
	Load() (map[string]*Workspace, error)
	Save(instances map[string]*Workspace) error
	LoadState() (*DaemonState, error)
	SaveState(state *DaemonState) error
	WithLock(fn func() error) error
}

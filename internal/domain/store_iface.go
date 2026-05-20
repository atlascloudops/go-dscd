package domain

// StateStore abstracts state persistence so domain logic doesn't depend on the store package.
type StateStore interface {
	Load() (map[string]*WorkspaceInstance, error)
	Save(instances map[string]*WorkspaceInstance) error
	WithLock(fn func() error) error
}

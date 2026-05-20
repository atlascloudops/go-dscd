package domain

// Reconciler checks actual filesystem state against persisted specs
// and updates WorkspaceInstance fields accordingly.
type Reconciler interface {
	Reconcile(instances map[string]*WorkspaceInstance) error
}

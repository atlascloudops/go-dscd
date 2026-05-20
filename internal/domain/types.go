package domain

import "time"

type WorkspaceState string

const (
	StatePending      WorkspaceState = "pending"
	StateProvisioning WorkspaceState = "provisioning"
	StateReady        WorkspaceState = "ready"
	StateError        WorkspaceState = "error"
)

// WorkspaceSpec is the input definition — what the client asks for.
type WorkspaceSpec struct {
	Name        string    `json:"name"`
	VCS         VCSTarget `json:"vcs"`
	PatName     string    `json:"pat_name"`
	ProjectRoot string    `json:"project_root"`
	Owner       string    `json:"owner"`
}

type VCSTarget struct {
	Host     string `json:"host"`
	AuthUser string `json:"auth_user"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	CloneURL string `json:"clone_url"`
}

// WorkspaceInstance is the realized state — what actually exists on the pod.
type WorkspaceInstance struct {
	Spec            WorkspaceSpec  `json:"spec"`
	State           WorkspaceState `json:"state"`
	CloneExists     bool           `json:"clone_exists"`
	CredentialHost  string         `json:"credential_host"`
	CredentialFresh bool           `json:"credential_fresh"`
	ProvisionedAt   *time.Time     `json:"provisioned_at,omitempty"`
	LastError       *string        `json:"last_error,omitempty"`
	LastReconcileAt *time.Time     `json:"last_reconcile_at,omitempty"`
}

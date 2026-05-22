package domain

const ResponseVersion = "v2"

type Response struct {
	Version string      `json:"version"`
	Command string      `json:"command"`
	Status  string      `json:"status"` // "ok" or "error"
	Error   *ErrorInfo  `json:"error"`
	Data    interface{} `json:"data"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

const (
	ErrSpecInvalid   = "SPEC_INVALID"
	ErrCloneFailed   = "CLONE_FAILED"
	ErrStateCorrupt  = "STATE_CORRUPT"
	ErrNotFound      = "NOT_FOUND"
	ErrAlreadyExists = "ALREADY_EXISTS"
	ErrLockFailed          = "LOCK_FAILED"
	ErrWorktreeDirty       = "WORKTREE_DIRTY"
	ErrCannotDeleteDefault = "CANNOT_DELETE_DEFAULT"
)

// IDEInfo is the clean response object for IDE state — exposed in inspect and
// provision responses. It collapses the internal IDEInstance (with its full event
// stream) into the fields the client needs: adapter name, port, and lifecycle
// status string.
type IDEInfo struct {
	Adapter string `json:"adapter"`
	Port    int    `json:"port"`
	Status  string `json:"status"`
}

// IDEInfoFromInstance builds an IDEInfo from an IDEInstance, or returns nil when
// the instance is nil. Status is the string representation of the IDE lifecycle.
func IDEInfoFromInstance(ide *IDEInstance) *IDEInfo {
	if ide == nil {
		return nil
	}
	status := string(ide.Status)
	if status == "" {
		status = string(StatusPending)
	}
	return &IDEInfo{
		Adapter: ide.Adapter,
		Port:    ide.Port,
		Status:  status,
	}
}

// WorkspaceInspectData extends WorkspaceInstance with worktree diagnostics for inspect responses.
// The IDEInfo field provides a clean adapter/port/status view when IDE state exists.
type WorkspaceInspectData struct {
	WorkspaceInstance
	BareRoot      string   `json:"bare_root"`
	WorktreeCount int      `json:"worktree_count"`
	Worktrees     []string `json:"worktrees"`
	IDEInfo       *IDEInfo `json:"ide_info,omitempty"`
}

// WorkspaceListItem is the per-workspace entry in a list response. It includes
// an optional IDEPort field (omitted when zero/IDE not ready) so clients can
// discover tunnel targets without inspecting each workspace individually.
type WorkspaceListItem struct {
	Spec       WorkspaceSpec          `json:"spec"`
	Status     Status                 `json:"status,omitempty"`
	HeadCommit string                 `json:"head_commit,omitempty"`
	IDEPort    int                    `json:"ide_port,omitempty"`
	Events     []WorkspaceEventRecord `json:"events,omitempty"`
}

// WorkspaceListItemFromInstance builds a WorkspaceListItem from an instance.
// IDEPort is set only when the IDE is active (status == Ready).
func WorkspaceListItemFromInstance(inst *WorkspaceInstance) WorkspaceListItem {
	item := WorkspaceListItem{
		Spec:       inst.Spec,
		Status:     inst.Status,
		HeadCommit: inst.HeadCommit,
		Events:     inst.Events,
	}
	if inst.IDE != nil && inst.IDE.Status == StatusReady {
		item.IDEPort = inst.IDE.Port
	}
	return item
}

func OkResponse(command string, data interface{}) Response {
	return Response{
		Version: ResponseVersion,
		Command: command,
		Status:  "ok",
		Error:   nil,
		Data:    data,
	}
}

func ErrorResponse(command string, err ErrorInfo) Response {
	return Response{
		Version: ResponseVersion,
		Command: command,
		Status:  "error",
		Error:   &err,
		Data:    nil,
	}
}

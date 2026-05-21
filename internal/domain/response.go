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

// WorkspaceInspectData extends WorkspaceInstance with worktree diagnostics for inspect responses.
type WorkspaceInspectData struct {
	WorkspaceInstance
	BareRoot      string   `json:"bare_root"`
	WorktreeCount int      `json:"worktree_count"`
	Worktrees     []string `json:"worktrees"`
	CredFresh     bool     `json:"credential_fresh"`
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

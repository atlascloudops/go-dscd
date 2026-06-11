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
	ErrSpecInvalid         = "SPEC_INVALID"
	ErrCloneFailed         = "CLONE_FAILED"
	ErrStateCorrupt        = "STATE_CORRUPT"
	ErrNotFound            = "NOT_FOUND"
	ErrAlreadyExists       = "ALREADY_EXISTS"
	ErrLockFailed          = "LOCK_FAILED"
	ErrWorktreeDirty       = "WORKTREE_DIRTY"
	ErrCannotDeleteDefault = "CANNOT_DELETE_DEFAULT"
)

// IDEInfo is the clean response object for IDE state.
type IDEInfo struct {
	Adapter string `json:"adapter"`
	Port    int    `json:"port"`
	Status  string `json:"status"`
}

// IDEInfoFromInstance builds an IDEInfo from an IDEInstance, or returns nil when nil.
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

// WorkspaceInspectData is the response shape for workspace inspect.
// It provides worktree details and IDE info from the aggregate.
type WorkspaceInspectData struct {
	Workspace
	WorktreeCount int      `json:"worktree_count"`
	IDEInfo       map[string]*IDEInfo `json:"ide_info,omitempty"`
	TemplateRepo  string   `json:"template_repo,omitempty"`
}

// WorkspaceListItem is the per-workspace entry in a list response.
type WorkspaceListItem struct {
	Name          string        `json:"name"`
	Repo          RepoInfo      `json:"repo"`
	Status        Status        `json:"status,omitempty"`
	WorktreeCount int           `json:"worktree_count"`
	IDEPort       int           `json:"ide_port,omitempty"`
	Events        []EventRecord `json:"events,omitempty"`
}

// WorkspaceListItemFromInstance builds a WorkspaceListItem from a Workspace.
func WorkspaceListItemFromInstance(ws *Workspace) WorkspaceListItem {
	item := WorkspaceListItem{
		Name:          ws.Name,
		Repo:          ws.Repo,
		Status:        ws.Status,
		WorktreeCount: len(ws.Worktrees),
		Events:        ws.Events,
	}
	// Surface the default worktree's IDE port when it is ready.
	if defWt := ws.DefaultWorktree(); defWt != nil {
		if ide := ws.IDEForWorktree(defWt.Name); ide != nil && ide.Status == StatusReady {
			item.IDEPort = ide.Port
		}
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

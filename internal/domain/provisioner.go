package domain

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Provisioner struct {
	StorePath     string         // path to state.json
	IDEAdapter    IDEAdapter     // optional; nil skips IDE phase
	PortAllocator *PortAllocator // optional; nil skips IDE phase
	ActivityLog   *ActivityLog   // optional; nil skips activity log writes
}

// recordWorkspaceEvent records a workspace event via the aggregate's RecordEvent
// method and appends the resulting EventRecord to the activity log.
func (p *Provisioner) recordWorkspaceEvent(w *Workspace, event WorkspaceEvent, detail string) {
	w.RecordEvent(event, detail)
	if p.ActivityLog != nil {
		_ = p.ActivityLog.Append(w.Events[len(w.Events)-1])
	}
}

// recordIDEEvent records an IDE event via the aggregate's RecordEvent method
// and appends the resulting EventRecord to the activity log.
func (p *Provisioner) recordIDEEvent(ide *IDEInstance, event IDEEvent, detail string) {
	ide.RecordEvent(event, detail)
	if p.ActivityLog != nil {
		_ = p.ActivityLog.Append(ide.Events[len(ide.Events)-1])
	}
}

// Provision creates a workspace using the new aggregate model.
// It always provisions a bare clone + default worktree on HEAD. Non-default
// worktrees are created lazily via AddWorktree. IDE startup is not part of
// provision — it is triggered separately via StartIDE (or the CLI's attach flow).
func (p *Provisioner) Provision(store StateStore, params ProvisionParams) (*Workspace, error) {
	spec := params.Spec
	if err := validateSpec(spec); err != nil {
		return nil, err
	}

	// Idempotency check — if default worktree already exists, return ready
	projectRoot := params.ProjectRoot()
	if worktreeExists(projectRoot) {
		return p.returnIdempotent(store, params)
	}

	// Template provisioning path
	if spec.Template != nil {
		return p.provisionTemplate(store, params)
	}

	return p.provisionBareCloneAndDefault(store, params)
}

// newWorkspaceFromParams creates a new Workspace aggregate from provision params.
func newWorkspaceFromParams(params ProvisionParams, now *time.Time) *Workspace {
	spec := params.Spec
	return &Workspace{
		Name:          spec.Name,
		Repo:          RepoInfo{Host: spec.VCS.Host, Slug: spec.VCS.Repo, CloneURL: spec.VCS.CloneURL},
		RepoRoot:      params.RepoRoot(),
		BareRoot:      params.BareRoot(),
		Owner:         spec.Owner,
		PatName:       spec.PatName,
		Template:      spec.Template,
		ProvisionedAt: now,
	}
}

// updateFromParams updates mutable aggregate fields from params (idempotent path).
func (ws *Workspace) updateFromParams(params ProvisionParams) {
	spec := params.Spec
	ws.Repo = RepoInfo{Host: spec.VCS.Host, Slug: spec.VCS.Repo, CloneURL: spec.VCS.CloneURL}
	ws.Owner = spec.Owner
	ws.PatName = spec.PatName
	ws.Template = spec.Template
	ws.RepoRoot = params.RepoRoot()
	ws.BareRoot = params.BareRoot()
}

// returnIdempotent handles the case where the worktree already exists.
func (p *Provisioner) returnIdempotent(store StateStore, params ProvisionParams) (*Workspace, error) {
	now := time.Now().UTC()

	var ws *Workspace
	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		if existing, ok := instances[params.Spec.Name]; ok {
			ws = existing
			ws.updateFromParams(params)
		} else {
			ws = newWorkspaceFromParams(params, &now)
			p.recordWorkspaceEvent(ws, EventWorktreeCreated, "detected by provision (idempotent)")
		}
		// Hydrate before resolving head commit
		if dirExists(params.BareRoot()) {
			p.hydrateWorktrees(ws)
		}
		// Update default worktree head commit
		if wt := ws.DefaultWorktree(); wt != nil {
			wt.HeadCommit = ResolveHeadCommit(wt.ProjectRoot, ws.Owner)
		}
		// Ensure default worktree is registered
		if ws.DefaultWorktree() == nil && params.ProjectRoot() != "" {
			ws.Worktrees = append(ws.Worktrees, Worktree{
				Name:        "default",
				ProjectRoot: params.ProjectRoot(),
				IsDefault:   true,
				HeadCommit:  ResolveHeadCommit(params.ProjectRoot(), ws.Owner),
			})
		}

		// IDE startup is separate — triggered via StartIDE or CLI attach flow

		instances[params.Spec.Name] = ws
		return store.Save(instances)
	}); err != nil {
		return nil, err
	}
	return ws, nil
}

// provisionBareCloneAndDefault creates a bare clone (if absent) and the default
// worktree on HEAD. This is the sole provision path — non-default worktrees are
// created lazily via AddWorktree.
func (p *Provisioner) provisionBareCloneAndDefault(store StateStore, params ProvisionParams) (*Workspace, error) {
	now := time.Now().UTC()
	spec := params.Spec

	ws := newWorkspaceFromParams(params, &now)

	// 1. mkdir -p <repo_root>
	if err := os.MkdirAll(params.RepoRoot(), 0755); err != nil {
		return nil, fmt.Errorf("create repo root: %w", err)
	}

	// 2. Clone bare repo if it doesn't already exist
	if !dirExists(params.BareRoot()) {
		p.recordWorkspaceEvent(ws, EventCloneStarted, spec.VCS.CloneURL)

		if err := p.bareClone(spec.VCS.CloneURL, params.BareRoot(), spec.Owner); err != nil {
			errMsg := fmt.Sprintf("git clone --bare failed: %v", err)
			p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
			ws.LastError = &errMsg
			p.persistState(store, spec.Name, ws)
			return ws, &ProvisionError{
				Code:    ErrCloneFailed,
				Message: "git clone --bare failed",
				Detail:  err.Error(),
			}
		}
		p.recordWorkspaceEvent(ws, EventCloneCompleted, "")
	}

	// 3. Resolve default branch from bare clone HEAD
	defaultBranch, err := resolveDefaultBranch(params.BareRoot(), spec.Owner)
	if err != nil {
		slog.Warn("could not resolve default branch, falling back to main", "workspace", spec.Name, "error", err)
		defaultBranch = "main"
	}

	// 4. Create default worktree: git -C <bare_root> worktree add <path> <branch>
	worktreePath := params.ProjectRoot()
	p.recordWorkspaceEvent(ws, EventWorktreeCreating, defaultBranch)
	if err := p.addWorktree(params.BareRoot(), worktreePath, defaultBranch, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, spec.Name, ws)
		return ws, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add (default) failed",
			Detail:  err.Error(),
		}
	}
	p.recordWorkspaceEvent(ws, EventWorktreeCreated, defaultBranch)

	// 5. Initialize submodules (best-effort, before hydrate)
	p.initSubmodules(ws, worktreePath)

	// 6. Populate the default worktree on the aggregate
	ws.Worktrees = []Worktree{
		{
			Name:        "default",
			Branch:      defaultBranch,
			ProjectRoot: worktreePath,
			IsDefault:   true,
			HeadCommit:  ResolveHeadCommit(worktreePath, spec.Owner),
		},
	}

	// 7. Hydrate (fetch + fast-forward pull on default worktree)
	p.hydrateWorktrees(ws)

	// IDE startup is deferred to the ide-worktree-scoping story

	if err := p.persistState(store, spec.Name, ws); err != nil {
		return nil, err
	}

	return ws, nil
}

// provisionTemplate creates a workspace from a template repository.
func (p *Provisioner) provisionTemplate(store StateStore, params ProvisionParams) (*Workspace, error) {
	now := time.Now().UTC()
	spec := params.Spec
	tmpl := spec.Template

	ws := newWorkspaceFromParams(params, &now)

	// 1. Clone template into a temporary directory
	p.recordWorkspaceEvent(ws, EventTemplateCloneStarted, tmpl.CloneURL)
	tmpDir := filepath.Join(params.RepoRoot(), ".template-tmp")
	if err := os.MkdirAll(params.RepoRoot(), 0755); err != nil {
		return nil, fmt.Errorf("create repo root: %w", err)
	}

	if err := p.cloneInto(tmpl.CloneURL, tmpDir, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("template clone failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, spec.Name, ws)
		return ws, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "template clone failed",
			Detail:  err.Error(),
		}
	}
	p.recordWorkspaceEvent(ws, EventTemplateCloneCompleted, "")

	// 2. Strip .git from the cloned content
	os.RemoveAll(filepath.Join(tmpDir, ".git"))

	// 3. Init a scratch repo, copy template files in, and commit
	scratchDir := filepath.Join(params.RepoRoot(), ".template-scratch")
	if err := p.gitInit(scratchDir, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git init (scratch) failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, spec.Name, ws)
		os.RemoveAll(tmpDir)
		return ws, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git init failed",
			Detail:  err.Error(),
		}
	}

	if err := p.copyDir(tmpDir, scratchDir); err != nil {
		errMsg := fmt.Sprintf("copy template files failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, spec.Name, ws)
		os.RemoveAll(tmpDir)
		os.RemoveAll(scratchDir)
		return ws, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "copy template files failed",
			Detail:  err.Error(),
		}
	}
	os.RemoveAll(tmpDir)

	commitMsg := fmt.Sprintf("Initial commit from template %s", tmpl.Repo)
	if err := p.gitAddAndCommit(scratchDir, commitMsg, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("initial commit failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, spec.Name, ws)
		os.RemoveAll(scratchDir)
		return ws, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "initial commit failed",
			Detail:  err.Error(),
		}
	}

	// 4. Clone bare from scratch, then create worktree
	if err := p.bareCloneLocal(scratchDir, params.BareRoot(), spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git clone --bare (from scratch) failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, spec.Name, ws)
		os.RemoveAll(scratchDir)
		return ws, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git clone --bare failed",
			Detail:  err.Error(),
		}
	}
	os.RemoveAll(scratchDir)

	// Remove the "origin" remote that clone --bare created (points to scratch)
	p.gitRemoteRemove(params.BareRoot(), "origin", spec.Owner)

	defaultBranch, err := resolveDefaultBranch(params.BareRoot(), spec.Owner)
	if err != nil {
		defaultBranch = "main"
	}

	worktreePath := filepath.Join(params.RepoRoot(), "default")
	if err := p.addWorktree(params.BareRoot(), worktreePath, defaultBranch, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, spec.Name, ws)
		return ws, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add (default) failed",
			Detail:  err.Error(),
		}
	}

	p.recordWorkspaceEvent(ws, EventTemplateReinitCompleted, tmpl.Repo)
	p.recordWorkspaceEvent(ws, EventWorktreeCreated, defaultBranch)

	// 5. Configure remotes on the bare repo
	p.gitRemoteAdd(params.BareRoot(), "template", tmpl.CloneURL, spec.Owner)
	p.gitRemoteSetPushURL(params.BareRoot(), "template", "no_push", spec.Owner)
	if spec.VCS.CloneURL != "" {
		p.gitRemoteAdd(params.BareRoot(), "origin", spec.VCS.CloneURL, spec.Owner)
	}

	// Populate worktrees
	ws.Worktrees = []Worktree{
		{
			Name:        "default",
			Branch:      defaultBranch,
			ProjectRoot: worktreePath,
			IsDefault:   true,
			HeadCommit:  ResolveHeadCommit(worktreePath, spec.Owner),
		},
	}

	// IDE startup is deferred to the ide-worktree-scoping story

	if err := p.persistState(store, spec.Name, ws); err != nil {
		return nil, err
	}

	return ws, nil
}

// WorktreeAddResult holds the outcome of an AddWorktree operation.
type WorktreeAddResult struct {
	WorkspaceName string `json:"workspace_name"`
	Branch        string `json:"branch"`
	ProjectRoot   string `json:"project_root"`
	Created       bool   `json:"created"` // false if worktree already existed (idempotent)
}

// AddWorktree lazily creates a worktree for a specific branch within an existing
// workspace's bare clone. It fetches the branch from origin, creates the worktree
// under <repo_root>/.worktrees/<branch>, appends a Worktree entry to the aggregate,
// and returns the project root path. The operation is idempotent — if a worktree for
// the branch already exists, the existing project root is returned without error.
func (p *Provisioner) AddWorktree(store StateStore, workspaceName, branch string) (*WorktreeAddResult, error) {
	instances, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	ws, ok := instances[workspaceName]
	if !ok {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("workspace '%s' not found", workspaceName),
		}
	}

	// Idempotency: if a worktree for this branch already exists, return it
	if existing := ws.FindWorktreeByBranch(branch); existing != nil {
		return &WorktreeAddResult{
			WorkspaceName: workspaceName,
			Branch:        branch,
			ProjectRoot:   existing.ProjectRoot,
			Created:       false,
		}, nil
	}

	// Also check on disk — the worktree may exist but not be in state
	projectRoot := DeriveProjectRoot(ws.RepoRoot, branch)
	if worktreeExists(projectRoot) {
		// Register it in state and return
		wt := Worktree{
			Name:        branch,
			Branch:      branch,
			ProjectRoot: projectRoot,
			IsDefault:   false,
			HeadCommit:  ResolveHeadCommit(projectRoot, ws.Owner),
		}
		ws.Worktrees = append(ws.Worktrees, wt)
		if err := p.persistState(store, workspaceName, ws); err != nil {
			return nil, err
		}
		return &WorktreeAddResult{
			WorkspaceName: workspaceName,
			Branch:        branch,
			ProjectRoot:   projectRoot,
			Created:       false,
		}, nil
	}

	// Ensure .worktrees/ directory exists
	worktreesDir := filepath.Join(ws.RepoRoot, ".worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return nil, fmt.Errorf("create .worktrees directory: %w", err)
	}

	// Fetch the branch from origin
	p.recordWorkspaceEvent(ws, EventWorktreeCreating, branch)
	p.fetchBranch(ws.BareRoot, branch, ws.Owner)

	// Create the worktree
	if err := p.addWorktree(ws.BareRoot, projectRoot, branch, ws.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		p.recordWorkspaceEvent(ws, EventProvisionFailed, errMsg)
		ws.LastError = &errMsg
		p.persistState(store, workspaceName, ws)
		return nil, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add failed",
			Detail:  err.Error(),
		}
	}

	// Initialize submodules in the new worktree (best-effort)
	p.initSubmodules(ws, projectRoot)

	// Append worktree entry to the aggregate
	wt := Worktree{
		Name:        branch,
		Branch:      branch,
		ProjectRoot: projectRoot,
		IsDefault:   false,
		HeadCommit:  ResolveHeadCommit(projectRoot, ws.Owner),
	}
	ws.Worktrees = append(ws.Worktrees, wt)
	p.recordWorkspaceEvent(ws, EventWorktreeCreated, branch)

	if err := p.persistState(store, workspaceName, ws); err != nil {
		return nil, err
	}

	slog.Info("worktree added", "workspace", workspaceName, "branch", branch, "project_root", projectRoot)

	return &WorktreeAddResult{
		WorkspaceName: workspaceName,
		Branch:        branch,
		ProjectRoot:   projectRoot,
		Created:       true,
	}, nil
}

// startIDEForWorktree allocates a port, starts the IDE adapter, and updates instance state.
func (p *Provisioner) startIDEForWorktree(ws *Workspace, worktreeName, worktreePath string) {
	if p.IDEAdapter == nil || p.PortAllocator == nil {
		return
	}

	key := PortKey(ws.Owner, worktreeName)
	port, err := p.PortAllocator.Allocate(key)
	if err != nil {
		ide := ws.IDEForWorktree(worktreeName)
		if ide == nil {
			ide = &IDEInstance{Name: ws.Name, Adapter: p.IDEAdapter.Name(), Port: 0}
			ws.SetIDEForWorktree(worktreeName, ide)
		}
		p.recordIDEEvent(ide, IDEEventFailed, fmt.Sprintf("port allocation: %v", err))
		return
	}

	ide := &IDEInstance{
		Name:    ws.Name,
		Adapter: p.IDEAdapter.Name(),
		Port:    port,
	}
	ws.SetIDEForWorktree(worktreeName, ide)

	ctx := IDEContext{
		Owner:        ws.Owner,
		WorktreePath: worktreePath,
		WorktreeName: worktreeName,
		Port:         port,
	}

	p.recordIDEEvent(ide, IDEEventStarted, fmt.Sprintf("port=%d", port))
	if err := p.IDEAdapter.Start(ctx); err != nil {
		p.recordIDEEvent(ide, IDEEventFailed, err.Error())
		return
	}

	p.recordIDEEvent(ide, IDEEventReady, fmt.Sprintf("port=%d", port))
}

// stopIDEForWorktree stops the IDE adapter and releases the port. Best-effort.
func (p *Provisioner) stopIDEForWorktree(ws *Workspace, worktreeName, worktreePath string) {
	ide := ws.IDEForWorktree(worktreeName)
	if ide == nil || p.IDEAdapter == nil || p.PortAllocator == nil {
		return
	}

	ctx := IDEContext{
		Owner:        ws.Owner,
		WorktreePath: worktreePath,
		WorktreeName: worktreeName,
		Port:         ide.Port,
	}

	if err := p.IDEAdapter.Stop(ctx); err != nil {
		slog.Error("ide stop failed", "workspace", ws.Name, "error", err)
	}

	key := PortKey(ws.Owner, worktreeName)
	if err := p.PortAllocator.Release(key); err != nil {
		slog.Error("port release failed", "workspace", ws.Name, "error", err)
	}

	p.recordIDEEvent(ide, IDEEventStopped, fmt.Sprintf("port=%d", ide.Port))
}

// healthCheckIDEForWorktree checks if a running IDE for a worktree is still healthy.
func (p *Provisioner) healthCheckIDEForWorktree(ws *Workspace, worktreeName string) {
	ide := ws.IDEForWorktree(worktreeName)
	if ide == nil || p.IDEAdapter == nil {
		return
	}

	wt := ws.FindWorktree(worktreeName)
	worktreePath := ""
	if wt != nil {
		worktreePath = wt.ProjectRoot
	}

	ctx := IDEContext{
		Owner:        ws.Owner,
		WorktreePath: worktreePath,
		WorktreeName: worktreeName,
		Port:         ide.Port,
	}

	err := p.IDEAdapter.HealthCheck(ctx)
	wasReady := ide.Status == StatusReady

	if err != nil && wasReady {
		p.recordIDEEvent(ide, IDEEventStopped, "health check failed")
	}
}

// IDEStartResult holds the outcome of a StartIDE operation.
type IDEStartResult struct {
	WorkspaceName string `json:"workspace_name"`
	WorktreeName  string `json:"worktree_name"`
	Adapter       string `json:"adapter"`
	Port          int    `json:"port"`
	Status        string `json:"status"`
}

// StartIDE starts an IDE instance for a specific worktree within a workspace.
// It allocates a port, starts the IDE adapter, records events, and persists state.
// The operation is idempotent — if an IDE is already running for the worktree,
// its current state is returned without restarting.
func (p *Provisioner) StartIDE(store StateStore, workspaceName, worktreeName string) (*IDEStartResult, error) {
	if p.IDEAdapter == nil || p.PortAllocator == nil {
		return nil, &ProvisionError{
			Code:    "IDE_NOT_CONFIGURED",
			Message: "IDE adapter or port allocator not configured",
		}
	}

	var result *IDEStartResult

	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}

		ws, ok := instances[workspaceName]
		if !ok {
			return &ProvisionError{
				Code:    ErrNotFound,
				Message: fmt.Sprintf("workspace '%s' not found", workspaceName),
			}
		}

		// Resolve the worktree — default to "default" if empty
		if worktreeName == "" {
			worktreeName = "default"
		}
		wt := ws.FindWorktree(worktreeName)
		if wt == nil {
			return &ProvisionError{
				Code:    ErrNotFound,
				Message: fmt.Sprintf("worktree '%s' not found in workspace '%s'", worktreeName, workspaceName),
			}
		}

		// Idempotent: if IDE is already running and ready, return current state
		if existing := ws.IDEForWorktree(worktreeName); existing != nil && existing.Status == StatusReady {
			result = &IDEStartResult{
				WorkspaceName: workspaceName,
				WorktreeName:  worktreeName,
				Adapter:       existing.Adapter,
				Port:          existing.Port,
				Status:        string(existing.Status),
			}
			return nil
		}

		p.startIDEForWorktree(ws, worktreeName, wt.ProjectRoot)

		ide := ws.IDEForWorktree(worktreeName)
		if ide == nil {
			return fmt.Errorf("IDE instance not created after start")
		}

		result = &IDEStartResult{
			WorkspaceName: workspaceName,
			WorktreeName:  worktreeName,
			Adapter:       ide.Adapter,
			Port:          ide.Port,
			Status:        string(ide.Status),
		}

		instances[workspaceName] = ws
		return store.Save(instances)
	}); err != nil {
		return nil, err
	}

	return result, nil
}

// IDEStopResult holds the outcome of a StopIDE operation.
type IDEStopResult struct {
	WorkspaceName string `json:"workspace_name"`
	WorktreeName  string `json:"worktree_name"`
	Message       string `json:"message"`
}

// StopIDE stops an IDE instance for a specific worktree within a workspace.
// It stops the IDE adapter, releases the port, records events, and persists state.
// If no IDE is running for the worktree, a not-found error is returned.
func (p *Provisioner) StopIDE(store StateStore, workspaceName, worktreeName string) (*IDEStopResult, error) {
	if p.IDEAdapter == nil || p.PortAllocator == nil {
		return nil, &ProvisionError{
			Code:    "IDE_NOT_CONFIGURED",
			Message: "IDE adapter or port allocator not configured",
		}
	}

	var result *IDEStopResult

	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}

		ws, ok := instances[workspaceName]
		if !ok {
			return &ProvisionError{
				Code:    ErrNotFound,
				Message: fmt.Sprintf("workspace '%s' not found", workspaceName),
			}
		}

		if worktreeName == "" {
			worktreeName = "default"
		}

		ide := ws.IDEForWorktree(worktreeName)
		if ide == nil {
			return &ProvisionError{
				Code:    ErrNotFound,
				Message: fmt.Sprintf("no IDE instance for worktree '%s' in workspace '%s'", worktreeName, workspaceName),
			}
		}

		wt := ws.FindWorktree(worktreeName)
		wtPath := ""
		if wt != nil {
			wtPath = wt.ProjectRoot
		}

		p.stopIDEForWorktree(ws, worktreeName, wtPath)

		result = &IDEStopResult{
			WorkspaceName: workspaceName,
			WorktreeName:  worktreeName,
			Message:       fmt.Sprintf("IDE stopped for worktree '%s' in workspace '%s'.", worktreeName, workspaceName),
		}

		instances[workspaceName] = ws
		return store.Save(instances)
	}); err != nil {
		return nil, err
	}

	return result, nil
}

// hydrateWorktrees fetches and fast-forward merges matching worktrees.
func (p *Provisioner) hydrateWorktrees(ws *Workspace) {
	p.recordWorkspaceEvent(ws, EventHydrateStarted, "")

	entries, err := ListWorktreeEntries(ws.BareRoot, ws.Owner)
	if err != nil {
		p.recordWorkspaceEvent(ws, EventHydrateSkipped, fmt.Sprintf("worktree list failed: %v", err))
		return
	}

	for _, entry := range entries {
		isDefault := filepath.Base(entry.Path) == "default"

		if !isDefault {
			continue
		}

		targetBranch := entry.Branch
		if targetBranch == "" {
			p.recordWorkspaceEvent(ws, EventHydrateSkipped, fmt.Sprintf("%s: detached HEAD", filepath.Base(entry.Path)))
			continue
		}

		dirty, dirtyErr := IsWorktreeDirty(entry.Path, ws.Owner)
		if dirtyErr != nil {
			p.recordWorkspaceEvent(ws, EventHydrateSkipped, fmt.Sprintf("%s: dirty check failed: %v", filepath.Base(entry.Path), dirtyErr))
			continue
		}
		if dirty {
			p.recordWorkspaceEvent(ws, EventHydrateSkipped, fmt.Sprintf("%s: uncommitted changes", filepath.Base(entry.Path)))
			continue
		}

		pullErr := p.ffPull(entry.Path, targetBranch, ws.Owner)
		if pullErr != nil {
			errStr := pullErr.Error()
			if strings.Contains(errStr, "Not possible to fast-forward") || strings.Contains(errStr, "fatal:") {
				p.recordWorkspaceEvent(ws, EventHydrateSkipped, fmt.Sprintf("%s: branch diverged, ff-only not possible", filepath.Base(entry.Path)))
			} else {
				p.recordWorkspaceEvent(ws, EventHydrateSkipped, fmt.Sprintf("%s: pull failed: %v", filepath.Base(entry.Path), pullErr))
			}
			continue
		}

		p.recordWorkspaceEvent(ws, EventHydrateCompleted, targetBranch)

		// After successful pull, sync and update submodules to pick up
		// any submodule ref changes from the pulled commits.
		p.syncAndUpdateSubmodules(ws, entry.Path)
	}
}

// ffPull runs git -C <worktreePath> pull --ff-only origin <branch> as owner.
func (p *Provisioner) ffPull(worktreePath, branch, owner string) error {
	pullCmd := fmt.Sprintf("git -C %s pull --ff-only origin %s", worktreePath, branch)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", pullCmd)
	} else {
		cmd = exec.Command("git", "-C", worktreePath, "pull", "--ff-only", "origin", branch)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// submoduleUpdate runs git -C <worktreePath> submodule update --init --recursive
// as owner. Best-effort: logs a warning on failure, does not return error.
// Repos without submodules are unaffected (the command is a no-op).
func (p *Provisioner) submoduleUpdate(worktreePath, owner string) error {
	subCmd := fmt.Sprintf("git -C %s submodule update --init --recursive", worktreePath)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", subCmd)
	} else {
		cmd = exec.Command("git", "-C", worktreePath, "submodule", "update", "--init", "--recursive")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// submoduleSync runs git -C <worktreePath> submodule sync --recursive as owner.
// Best-effort: logs a warning on failure, does not return error.
// This should be called before submoduleUpdate after a pull to handle URL
// changes in .gitmodules.
func (p *Provisioner) submoduleSync(worktreePath, owner string) error {
	syncCmd := fmt.Sprintf("git -C %s submodule sync --recursive", worktreePath)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", syncCmd)
	} else {
		cmd = exec.Command("git", "-C", worktreePath, "submodule", "sync", "--recursive")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// initSubmodules runs submodule update --init --recursive on a worktree path,
// recording events on the workspace aggregate. Best-effort: failures are logged
// and recorded as skipped events but do not block provisioning.
func (p *Provisioner) initSubmodules(ws *Workspace, worktreePath string) {
	p.recordWorkspaceEvent(ws, EventSubmoduleInitStarted, worktreePath)

	if err := p.submoduleUpdate(worktreePath, ws.Owner); err != nil {
		slog.Warn("submodule update failed", "workspace", ws.Name, "path", worktreePath, "error", err)
		p.recordWorkspaceEvent(ws, EventSubmoduleInitSkipped, fmt.Sprintf("submodule update failed: %v", err))
		return
	}

	p.recordWorkspaceEvent(ws, EventSubmoduleInitCompleted, worktreePath)
}

// syncAndUpdateSubmodules runs submodule sync then update on a worktree path
// after a hydration pull. Best-effort: failures are logged and recorded as
// skipped events.
func (p *Provisioner) syncAndUpdateSubmodules(ws *Workspace, worktreePath string) {
	p.recordWorkspaceEvent(ws, EventSubmoduleInitStarted, worktreePath)

	if err := p.submoduleSync(worktreePath, ws.Owner); err != nil {
		slog.Warn("submodule sync failed", "workspace", ws.Name, "path", worktreePath, "error", err)
		p.recordWorkspaceEvent(ws, EventSubmoduleInitSkipped, fmt.Sprintf("submodule sync failed: %v", err))
		return
	}

	if err := p.submoduleUpdate(worktreePath, ws.Owner); err != nil {
		slog.Warn("submodule update failed after sync", "workspace", ws.Name, "path", worktreePath, "error", err)
		p.recordWorkspaceEvent(ws, EventSubmoduleInitSkipped, fmt.Sprintf("submodule update failed: %v", err))
		return
	}

	p.recordWorkspaceEvent(ws, EventSubmoduleInitCompleted, worktreePath)
}

// DeprovisionResult holds the outcome of a deprovision operation.
type DeprovisionResult struct {
	Removed          []string `json:"removed"`                     // workspace names removed
	RemovedWorktrees []string `json:"removed_worktrees,omitempty"` // worktree names removed
	Message          string   `json:"message"`
}

// Deprovision removes an entire workspace aggregate: stops all IDE instances,
// removes all worktrees, removes the bare clone, removes the repo container
// directory, and deletes the state entry.
func (p *Provisioner) Deprovision(store StateStore, name string, force bool) (*DeprovisionResult, error) {
	instances, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	ws, ok := instances[name]
	if !ok {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("workspace '%s' not found", name),
		}
	}

	// Guard: check for uncommitted changes in all worktrees (unless --force)
	if !force {
		for _, wt := range ws.Worktrees {
			dirty, dirtyErr := IsWorktreeDirty(wt.ProjectRoot, ws.Owner)
			if dirtyErr == nil && dirty {
				return nil, &ProvisionError{
					Code:    ErrWorktreeDirty,
					Message: fmt.Sprintf("Worktree '%s' has uncommitted changes. Use --force to delete.", wt.Name),
				}
			}
		}
	}

	// Stop all IDE instances
	for wtName := range ws.IDE {
		wt := ws.FindWorktree(wtName)
		wtPath := ""
		if wt != nil {
			wtPath = wt.ProjectRoot
		}
		p.stopIDEForWorktree(ws, wtName, wtPath)
	}

	// Collect worktree names for the result
	var removedWorktrees []string
	for _, wt := range ws.Worktrees {
		removedWorktrees = append(removedWorktrees, wt.Name)
	}

	// Remove all worktrees via git (non-default first, default last)
	for _, wt := range ws.Worktrees {
		if wt.IsDefault {
			continue
		}
		_ = p.removeWorktree(ws.BareRoot, wt.ProjectRoot, ws.Owner, force)
	}
	for _, wt := range ws.Worktrees {
		if !wt.IsDefault {
			continue
		}
		_ = p.removeWorktree(ws.BareRoot, wt.ProjectRoot, ws.Owner, force)
	}

	// Remove bare clone and repo container directory
	if ws.BareRoot != "" {
		os.RemoveAll(ws.BareRoot)
	}
	if ws.RepoRoot != "" {
		os.RemoveAll(ws.RepoRoot)
	}

	// Remove state entry
	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		delete(instances, name)
		return store.Save(instances)
	}); err != nil {
		return nil, fmt.Errorf("remove state entry: %w", err)
	}

	slog.Info("workspace removed", "workspace", name, "phase", "deprovision")

	suffix := ""
	if force {
		suffix = " (forced)"
	}
	return &DeprovisionResult{
		Removed:          []string{name},
		RemovedWorktrees: removedWorktrees,
		Message:          fmt.Sprintf("Workspace '%s' removed%s.", name, suffix),
	}, nil
}

// DeprovisionWorktree removes a single worktree from a workspace aggregate by branch name.
// It stops the IDE instance for that worktree, runs `git worktree remove`, removes the
// worktree entry from the slice and the IDE entry from the map, and saves the updated aggregate.
// The default worktree cannot be removed — users must deprovision the entire workspace instead.
func (p *Provisioner) DeprovisionWorktree(store StateStore, name, branch string, force bool) (*DeprovisionResult, error) {
	instances, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	ws, ok := instances[name]
	if !ok {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("workspace '%s' not found", name),
		}
	}

	// Find the worktree by branch name
	wt := ws.FindWorktreeByBranch(branch)
	if wt == nil {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("worktree for branch '%s' not found in workspace '%s'", branch, name),
		}
	}

	// Cannot remove the default worktree
	if wt.IsDefault {
		return nil, &ProvisionError{
			Code:    ErrCannotDeleteDefault,
			Message: fmt.Sprintf("Cannot remove the default worktree. Use 'workspace deprovision %s' to remove the entire workspace.", name),
		}
	}

	// Guard: check for uncommitted changes (unless --force)
	if !force {
		dirty, dirtyErr := IsWorktreeDirty(wt.ProjectRoot, ws.Owner)
		if dirtyErr == nil && dirty {
			return nil, &ProvisionError{
				Code:    ErrWorktreeDirty,
				Message: fmt.Sprintf("Worktree '%s' has uncommitted changes. Use --force to delete.", wt.Name),
			}
		}
	}

	// Stop IDE for this worktree
	p.stopIDEForWorktree(ws, wt.Name, wt.ProjectRoot)

	// Remove the worktree via git
	wtName := wt.Name
	if err := p.removeWorktree(ws.BareRoot, wt.ProjectRoot, ws.Owner, force); err != nil {
		slog.Error("git worktree remove failed", "workspace", name, "worktree", wtName, "error", err)
	}

	// Remove worktree entry from the aggregate
	ws.RemoveWorktreeByName(wtName)
	ws.RemoveIDEForWorktree(wtName)

	// Save the updated aggregate
	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		instances[name] = ws
		return store.Save(instances)
	}); err != nil {
		return nil, fmt.Errorf("update state: %w", err)
	}

	slog.Info("worktree removed", "workspace", name, "worktree", wtName, "phase", "deprovision")

	suffix := ""
	if force {
		suffix = " (forced)"
	}
	return &DeprovisionResult{
		Removed:          []string{name},
		RemovedWorktrees: []string{wtName},
		Message:          fmt.Sprintf("Worktree '%s' removed from workspace '%s'%s.", wtName, name, suffix),
	}, nil
}

// Prune removes all clean non-default worktrees within a single workspace aggregate.
// It operates on the Worktrees slice, not on separate state entries.
func (p *Provisioner) Prune(store StateStore, repoName string) (*PruneResult, error) {
	instances, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	ws, ok := instances[repoName]
	if !ok {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("no workspaces found for '%s'", repoName),
		}
	}

	result := &PruneResult{
		Pruned:  []string{},
		Skipped: []PruneSkipped{},
	}

	// Collect non-default worktrees from the aggregate's Worktrees slice
	var nonDefaultWorktrees []Worktree
	for _, wt := range ws.Worktrees {
		if !wt.IsDefault {
			nonDefaultWorktrees = append(nonDefaultWorktrees, wt)
		}
	}

	if len(nonDefaultWorktrees) == 0 {
		result.Message = "No non-default worktrees to prune."
		return result, nil
	}

	// Prune each non-default worktree within the aggregate
	var prunedNames []string
	for _, wt := range nonDefaultWorktrees {
		if wt.ProjectRoot != "" {
			dirty, dirtyErr := IsWorktreeDirty(wt.ProjectRoot, ws.Owner)
			if dirtyErr == nil && dirty {
				result.Skipped = append(result.Skipped, PruneSkipped{
					Name:   wt.Name,
					Reason: "uncommitted changes",
				})
				continue
			}
		}

		// Stop IDE for this worktree
		p.stopIDEForWorktree(ws, wt.Name, wt.ProjectRoot)

		// Remove the worktree via git
		if wt.ProjectRoot != "" {
			if err := p.removeWorktree(ws.BareRoot, wt.ProjectRoot, ws.Owner, false); err != nil {
				result.Skipped = append(result.Skipped, PruneSkipped{
					Name:   wt.Name,
					Reason: fmt.Sprintf("remove failed: %v", err),
				})
				continue
			}
		}

		result.Pruned = append(result.Pruned, wt.Name)
		prunedNames = append(prunedNames, wt.Name)
		slog.Info("worktree pruned", "workspace", repoName, "worktree", wt.Name, "phase", "prune")
	}

	if ws.BareRoot != "" {
		p.pruneWorktrees(ws.BareRoot, ws.Owner)
	}

	// Update the aggregate: remove pruned worktrees from slice and IDE map
	if len(prunedNames) > 0 {
		for _, name := range prunedNames {
			ws.RemoveWorktreeByName(name)
			ws.RemoveIDEForWorktree(name)
		}

		if err := store.WithLock(func() error {
			instances, err := store.Load()
			if err != nil {
				return err
			}
			instances[repoName] = ws
			return store.Save(instances)
		}); err != nil {
			return nil, fmt.Errorf("update state: %w", err)
		}
	}

	prunedCount := len(result.Pruned)
	skippedCount := len(result.Skipped)

	if skippedCount == 0 {
		result.Message = fmt.Sprintf("%d worktree%s pruned.", prunedCount, pluralS(prunedCount))
	} else {
		result.Message = fmt.Sprintf("%d worktree%s pruned, %d skipped.", prunedCount, pluralS(prunedCount), skippedCount)
	}

	return result, nil
}

// pluralS returns "s" if count != 1, empty string otherwise.
func pluralS(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// removeWorktree runs git -C <bareRoot> worktree remove <projectRoot> [--force] as owner.
func (p *Provisioner) removeWorktree(bareRoot, projectRoot, owner string, force bool) error {
	args := []string{"-C", bareRoot, "worktree", "remove", projectRoot}
	if force {
		args = append(args, "--force")
	}

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		gitCmd := fmt.Sprintf("git -C %s worktree remove %s", bareRoot, projectRoot)
		if force {
			gitCmd += " --force"
		}
		cmd = exec.Command("su", "-", owner, "-c", gitCmd)
	} else {
		cmd = exec.Command("git", args...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// pruneWorktrees runs git -C <bareRoot> worktree prune as owner.
func (p *Provisioner) pruneWorktrees(bareRoot, owner string) {
	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c",
			fmt.Sprintf("git -C %s worktree prune", bareRoot))
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "worktree", "prune")
	}
	_ = cmd.Run()
}

// bareClone runs git clone --bare as the owner.
func (p *Provisioner) bareClone(cloneURL, bareRoot, owner string) error {
	cloneCmd := fmt.Sprintf("git clone --bare %s %s", cloneURL, bareRoot)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", cloneCmd)
	} else {
		cmd = exec.Command("git", "clone", "--bare", cloneURL, bareRoot)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// addWorktree runs git -C <bareRoot> worktree add [-f] <path> <branch> as the owner.
func (p *Provisioner) addWorktree(bareRoot, worktreePath, branch, owner string) error {
	wtCmd := fmt.Sprintf("git -C %s worktree add -f %s %s", bareRoot, worktreePath, branch)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", wtCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "worktree", "add", "-f", worktreePath, branch)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// fetchBranch attempts to fetch a specific branch from origin. Non-fatal if it fails.
func (p *Provisioner) fetchBranch(bareRoot, branch, owner string) {
	fetchCmd := fmt.Sprintf("git -C %s fetch origin %s", bareRoot, branch)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", fetchCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "fetch", "origin", branch)
	}

	_ = cmd.Run()
}

// resolveDefaultBranch reads the HEAD symbolic ref from a bare clone.
func resolveDefaultBranch(bareRoot, owner string) (string, error) {
	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c",
			fmt.Sprintf("git -C %s symbolic-ref --short HEAD", bareRoot))
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "symbolic-ref", "--short", "HEAD")
	}

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("symbolic-ref failed: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" {
		return "", fmt.Errorf("symbolic-ref returned empty string")
	}
	return branch, nil
}

// worktreeExists checks if a worktree (or clone) exists at the given path.
func worktreeExists(projectRoot string) bool {
	gitPath := filepath.Join(projectRoot, ".git")
	_, err := os.Stat(gitPath)
	return err == nil
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func (p *Provisioner) persistState(store StateStore, name string, ws *Workspace) error {
	return store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		instances[name] = ws
		return store.Save(instances)
	})
}

func validateSpec(spec WorkspaceSpec) error {
	var missing []string
	if spec.Name == "" {
		missing = append(missing, "name")
	}
	if spec.VCS.CloneURL == "" && spec.Template == nil {
		missing = append(missing, "vcs.clone_url")
	}
	if spec.Owner == "" {
		missing = append(missing, "owner")
	}
	if len(missing) > 0 {
		return &ProvisionError{
			Code:    ErrSpecInvalid,
			Message: "missing required fields",
			Detail:  strings.Join(missing, ", "),
		}
	}

	if err := validateName(spec.Name); err != nil {
		return err
	}

	if spec.Template != nil {
		if spec.Template.CloneURL == "" {
			return &ProvisionError{
				Code:    ErrSpecInvalid,
				Message: "template.clone_url is required when template is set",
			}
		}
	}

	return nil
}

// validateName checks workspace name safety.
// Names follow the pattern "repo" or "repo/worktree" (at most 2 segments).
func validateName(name string) error {
	if len(name) > 128 {
		return &ProvisionError{
			Code:    ErrSpecInvalid,
			Message: "name too long",
			Detail:  fmt.Sprintf("name must be <= 128 characters, got %d", len(name)),
		}
	}

	const unsafeChars = "\x00\\:*?\"<>|&;`$!#{}[]()'\n\r\t"
	for _, ch := range name {
		if strings.ContainsRune(unsafeChars, ch) {
			return &ProvisionError{
				Code:    ErrSpecInvalid,
				Message: "name contains unsafe character",
				Detail:  fmt.Sprintf("character %q (U+%04X) is not allowed in workspace names", ch, ch),
			}
		}
	}

	// Segment validation: at most 2 segments (repo/worktree)
	segments := strings.Split(name, "/")
	if len(segments) > 2 {
		return &ProvisionError{
			Code:    ErrSpecInvalid,
			Message: "name has too many path segments",
			Detail:  fmt.Sprintf("at most 2 segments allowed (repo/worktree), got %d", len(segments)),
		}
	}

	// No empty segments (leading/trailing slash, double slash)
	for _, seg := range segments {
		if seg == "" {
			return &ProvisionError{
				Code:    ErrSpecInvalid,
				Message: "name contains empty segment",
				Detail:  "each path segment must be non-empty",
			}
		}
	}

	// No path traversal
	for _, seg := range segments {
		if seg == "." || seg == ".." {
			return &ProvisionError{
				Code:    ErrSpecInvalid,
				Message: "name contains path traversal",
				Detail:  fmt.Sprintf("segment %q is not allowed", seg),
			}
		}
	}

	return nil
}

func ResolveHeadCommit(projectRoot, owner string) string {
	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c",
			fmt.Sprintf("git -C %s rev-parse HEAD", projectRoot))
	} else {
		cmd = exec.Command("git", "-C", projectRoot, "rev-parse", "HEAD")
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "root"
}

type ProvisionError struct {
	Code    string
	Message string
	Detail  string
}

func (e *ProvisionError) Error() string {
	return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Detail)
}

// cloneInto runs git clone <url> <dest> as the spec owner.
func (p *Provisioner) cloneInto(url, dest, owner string) error {
	cloneCmd := fmt.Sprintf("git clone --recurse-submodules %s %s", url, dest)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", cloneCmd)
	} else {
		cmd = exec.Command("git", "clone", "--recurse-submodules", url, dest)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// gitInit runs git init <path> as the owner (non-bare).
func (p *Provisioner) gitInit(path, owner string) error {
	initCmd := fmt.Sprintf("git init %s", path)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", initCmd)
	} else {
		cmd = exec.Command("git", "init", path)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// bareCloneLocal runs git clone --bare <src> <dst> as the owner.
func (p *Provisioner) bareCloneLocal(src, dst, owner string) error {
	cloneCmd := fmt.Sprintf("git clone --bare %s %s", src, dst)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", cloneCmd)
	} else {
		cmd = exec.Command("git", "clone", "--bare", src, dst)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// gitRemoteRemove runs git -C <bareRoot> remote remove <name> as owner.
func (p *Provisioner) gitRemoteRemove(bareRoot, name, owner string) {
	remoteCmd := fmt.Sprintf("git -C %s remote remove %s", bareRoot, name)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", remoteCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "remote", "remove", name)
	}

	_ = cmd.Run()
}

// copyDir copies all files and directories from src into dst.
func (p *Provisioner) copyDir(src, dst string) error {
	cmd := exec.Command("cp", "-a", src+"/.", dst+"/")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// gitRemoteAdd runs git -C <bareRoot> remote add <name> <url> as owner.
func (p *Provisioner) gitRemoteAdd(bareRoot, name, url, owner string) {
	remoteCmd := fmt.Sprintf("git -C %s remote add %s %s", bareRoot, name, url)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", remoteCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "remote", "add", name, url)
	}

	_ = cmd.Run()
}

// gitRemoteSetPushURL runs git -C <bareRoot> remote set-url --push <name> <url> as owner.
func (p *Provisioner) gitRemoteSetPushURL(bareRoot, name, url, owner string) {
	remoteCmd := fmt.Sprintf("git -C %s remote set-url --push %s %s", bareRoot, name, url)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", remoteCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "remote", "set-url", "--push", name, url)
	}

	_ = cmd.Run()
}

// gitAddAndCommit runs git add . && git commit in the worktree as owner.
func (p *Provisioner) gitAddAndCommit(worktreePath, message, owner string) error {
	addCommitCmd := fmt.Sprintf("cd %s && git add . && git -c user.name=dscd -c user.email=dscd@local commit -m %q", worktreePath, message)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", addCommitCmd)
	} else {
		cmd = exec.Command("sh", "-c", addCommitCmd)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// ResolveTemplateRepo reads the template remote URL from a bare repo.
func ResolveTemplateRepo(bareRoot, owner string) string {
	remoteCmd := fmt.Sprintf("git -C %s remote get-url template", bareRoot)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", remoteCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "remote", "get-url", "template")
	}

	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

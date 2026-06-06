package domain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Provisioner struct {
	StorePath     string         // path to state.json
	LogDir        string         // /opt/dsc/var/dscd/logs/
	IDEAdapter    IDEAdapter     // optional; nil skips IDE phase
	PortAllocator *PortAllocator // optional; nil skips IDE phase
}

// Provision creates a workspace using dual-mode provisioning:
//   - If IsDefault and no bare clone exists: bare clone + default worktree
//   - If bare clone exists (or is created): add worktree from existing bare
func (p *Provisioner) Provision(store StateStore, spec WorkspaceSpec) (*Workspace, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	if err := p.resolveIDEAdapter(spec); err != nil {
		return nil, err
	}

	// Idempotency check — if worktree already exists, return ready
	if worktreeExists(spec.ProjectRoot) {
		return p.returnIdempotent(store, spec)
	}

	// Template provisioning path: reinit git history from template repo
	if spec.Template != nil {
		return p.provisionTemplate(store, spec)
	}

	bareExists := dirExists(spec.BareRoot)

	if spec.IsDefault && !bareExists {
		return p.provisionBareCloneAndDefault(store, spec)
	}

	return p.provisionWorktree(store, spec, !bareExists)
}

// returnIdempotent handles the case where the worktree already exists.
func (p *Provisioner) returnIdempotent(store StateStore, spec WorkspaceSpec) (*Workspace, error) {
	now := time.Now().UTC()
	p.writeLog(spec.Name, "provision", "Worktree already exists at %s, skipping", spec.ProjectRoot)

	// Load existing instance to preserve event history
	var inst *Workspace
	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		if existing, ok := instances[spec.Name]; ok {
			inst = existing
			inst.Spec = spec
		} else {
			inst = &Workspace{
				Spec:          spec,
						ProvisionedAt:  &now,
			}
			inst.RecordEvent(EventWorktreeCreated, "detected by provision (idempotent)")
		}
		// Hydrate before resolving head commit so it reflects the latest state
		if dirExists(spec.BareRoot) {
			p.hydrate(inst, spec)
		}
		inst.HeadCommit = ResolveHeadCommit(spec.ProjectRoot, spec.Owner)

		// IDE: if requested but not running, start; if running, health-check
		if spec.IDE != nil && p.IDEAdapter != nil {
			if inst.IDE == nil || inst.IDE.Status != StatusReady {
				p.startIDE(inst, spec)
			} else {
				p.healthCheckIDE(inst)
			}
		}

		instances[spec.Name] = inst
		return store.Save(instances)
	}); err != nil {
		return nil, err
	}
	p.writeLog(spec.Name, "provision", "Lifecycle: %s (idempotent)", inst.Status)
	return inst, nil
}

// provisionBareCloneAndDefault creates a bare clone and the default worktree.
func (p *Provisioner) provisionBareCloneAndDefault(store StateStore, spec WorkspaceSpec) (*Workspace, error) {
	now := time.Now().UTC()
	p.writeLog(spec.Name, "provision", "Bare-cloning %s into %s", spec.VCS.CloneURL, spec.BareRoot)

	inst := &Workspace{
		Spec:           spec,
		ProvisionedAt:  &now,
	}
	inst.RecordEvent(EventCloneStarted, spec.VCS.CloneURL)

	// 1. mkdir -p <repo_root>
	if err := os.MkdirAll(spec.RepoRoot, 0755); err != nil {
		return nil, fmt.Errorf("create repo root: %w", err)
	}

	// 2. git clone --bare <clone_url> <bare_root>
	if err := p.bareClone(spec); err != nil {
		errMsg := fmt.Sprintf("git clone --bare failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git clone --bare failed",
			Detail:  err.Error(),
		}
	}
	inst.RecordEvent(EventCloneCompleted, "")

	// 3. Resolve default branch
	defaultBranch, err := resolveDefaultBranch(spec.BareRoot, spec.Owner)
	if err != nil {
		p.writeLog(spec.Name, "provision", "Could not resolve default branch, falling back to main: %v", err)
		defaultBranch = "main"
	}
	p.writeLog(spec.Name, "provision", "Default branch resolved: %s", defaultBranch)

	// 4. git -C <bare_root> worktree add ../default <default_branch>
	inst.RecordEvent(EventWorktreeCreating, defaultBranch)
	worktreePath := filepath.Join(spec.RepoRoot, "default")
	if err := p.addWorktree(spec.BareRoot, worktreePath, defaultBranch, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add (default) failed",
			Detail:  err.Error(),
		}
	}
	inst.RecordEvent(EventWorktreeCreated, defaultBranch)

	p.writeLog(spec.Name, "provision", "Bare clone + default worktree complete")

	p.hydrate(inst, spec)
	inst.HeadCommit = ResolveHeadCommit(spec.ProjectRoot, spec.Owner)
	p.startIDE(inst, spec)

	if err := p.persistState(store, spec.Name, inst); err != nil {
		return nil, err
	}

	p.writeLog(spec.Name, "provision", "Lifecycle: %s", inst.Status)
	return inst, nil
}

// provisionTemplate creates a workspace from a template repository.
// It clones the template, strips .git, reinitialises as a bare+worktree,
// copies template files, configures remotes, and makes an initial commit.
//
// Strategy (compatible with git < 2.42 which lacks --orphan):
//  1. Clone template into temp dir, strip .git
//  2. git init a scratch repo, copy template files, commit
//  3. git clone --bare scratch into .bare/
//  4. Remove scratch, add worktree from bare
//  5. Configure remotes on the bare repo
func (p *Provisioner) provisionTemplate(store StateStore, spec WorkspaceSpec) (*Workspace, error) {
	now := time.Now().UTC()
	tmpl := spec.Template
	p.writeLog(spec.Name, "provision", "Template provisioning from %s", tmpl.CloneURL)

	inst := &Workspace{
		Spec:           spec,
		ProvisionedAt:  &now,
	}

	// 1. Clone template into a temporary directory
	inst.RecordEvent(EventTemplateCloneStarted, tmpl.CloneURL)
	tmpDir := filepath.Join(spec.RepoRoot, ".template-tmp")
	if err := os.MkdirAll(spec.RepoRoot, 0755); err != nil {
		return nil, fmt.Errorf("create repo root: %w", err)
	}

	if err := p.cloneInto(tmpl.CloneURL, tmpDir, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("template clone failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "template clone failed",
			Detail:  err.Error(),
		}
	}
	inst.RecordEvent(EventTemplateCloneCompleted, "")
	p.writeLog(spec.Name, "provision", "Template cloned to %s", tmpDir)

	// 2. Strip .git from the cloned content
	os.RemoveAll(filepath.Join(tmpDir, ".git"))

	// 3. Init a scratch repo, copy template files in, and commit
	scratchDir := filepath.Join(spec.RepoRoot, ".template-scratch")
	if err := p.gitInit(scratchDir, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git init (scratch) failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		os.RemoveAll(tmpDir)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git init failed",
			Detail:  err.Error(),
		}
	}

	if err := p.copyDir(tmpDir, scratchDir); err != nil {
		errMsg := fmt.Sprintf("copy template files failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		os.RemoveAll(tmpDir)
		os.RemoveAll(scratchDir)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "copy template files failed",
			Detail:  err.Error(),
		}
	}
	os.RemoveAll(tmpDir)

	commitMsg := fmt.Sprintf("Initial commit from template %s", tmpl.Repo)
	if err := p.gitAddAndCommit(scratchDir, commitMsg, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("initial commit failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		os.RemoveAll(scratchDir)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "initial commit failed",
			Detail:  err.Error(),
		}
	}

	// 4. Clone bare from scratch, then create worktree
	if err := p.bareCloneLocal(scratchDir, spec.BareRoot, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git clone --bare (from scratch) failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		os.RemoveAll(scratchDir)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git clone --bare failed",
			Detail:  err.Error(),
		}
	}
	os.RemoveAll(scratchDir)

	// Remove the "origin" remote that clone --bare created (points to scratch)
	p.gitRemoteRemove(spec.BareRoot, "origin", spec.Owner)

	defaultBranch, err := resolveDefaultBranch(spec.BareRoot, spec.Owner)
	if err != nil {
		defaultBranch = "main"
	}

	worktreePath := filepath.Join(spec.RepoRoot, "default")
	if err := p.addWorktree(spec.BareRoot, worktreePath, defaultBranch, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add (default) failed",
			Detail:  err.Error(),
		}
	}

	inst.RecordEvent(EventTemplateReinitCompleted, tmpl.Repo)
	inst.RecordEvent(EventWorktreeCreated, defaultBranch)

	// 5. Configure remotes on the bare repo
	p.gitRemoteAdd(spec.BareRoot, "template", tmpl.CloneURL, spec.Owner)
	p.gitRemoteSetPushURL(spec.BareRoot, "template", "no_push", spec.Owner)
	if spec.VCS.CloneURL != "" {
		p.gitRemoteAdd(spec.BareRoot, "origin", spec.VCS.CloneURL, spec.Owner)
	}

	p.writeLog(spec.Name, "provision", "Template provisioning complete")

	inst.HeadCommit = ResolveHeadCommit(spec.ProjectRoot, spec.Owner)
	p.startIDE(inst, spec)

	if err := p.persistState(store, spec.Name, inst); err != nil {
		return nil, err
	}

	p.writeLog(spec.Name, "provision", "Lifecycle: %s", inst.Status)
	return inst, nil
}

// cloneInto runs git clone <url> <dest> as the spec owner.
func (p *Provisioner) cloneInto(url, dest, owner string) error {
	cloneCmd := fmt.Sprintf("git clone %s %s", url, dest)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", cloneCmd)
	} else {
		cmd = exec.Command("git", "clone", url, dest)
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
	// Use cp -a for reliable recursive copy with permissions preserved
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

// resolveTemplateRepo reads the template remote URL from a bare repo.
// Returns empty string if the "template" remote does not exist.
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

// provisionWorktree adds a worktree from an existing bare clone.
// If needsBareClone is true, the bare clone is created first with clone events.
func (p *Provisioner) provisionWorktree(store StateStore, spec WorkspaceSpec, needsBareClone bool) (*Workspace, error) {
	now := time.Now().UTC()
	p.writeLog(spec.Name, "provision", "Adding worktree %s (branch: %s)", spec.WorktreeName, spec.VCS.Branch)

	inst := &Workspace{
		Spec:           spec,
		ProvisionedAt:  &now,
	}

	// If bare clone doesn't exist yet, create it with events
	if needsBareClone {
		inst.RecordEvent(EventCloneStarted, spec.VCS.CloneURL)
		if err := p.bareClone(spec); err != nil {
			errMsg := fmt.Sprintf("git clone --bare failed: %v", err)
			inst.RecordEvent(EventProvisionFailed, errMsg)
			inst.LastError = &errMsg
			p.persistState(store, spec.Name, inst)
			p.writeLog(spec.Name, "error", "%s", errMsg)
			return inst, &ProvisionError{
				Code:    ErrCloneFailed,
				Message: "git clone --bare failed",
				Detail:  err.Error(),
			}
		}
		inst.RecordEvent(EventCloneCompleted, "")
	}

	// Determine worktree path
	var worktreePath string
	if spec.IsDefault {
		worktreePath = filepath.Join(spec.RepoRoot, "default")
	} else {
		// mkdir -p .worktrees/
		worktreesDir := filepath.Join(spec.RepoRoot, ".worktrees")
		if err := os.MkdirAll(worktreesDir, 0755); err != nil {
			return nil, fmt.Errorf("create .worktrees dir: %w", err)
		}
		worktreePath = filepath.Join(worktreesDir, spec.WorktreeName)
	}

	// Fetch the branch if it doesn't exist locally
	p.fetchBranch(spec.BareRoot, spec.VCS.Branch, spec.Owner)

	// git -C <bare_root> worktree add <path> <branch>
	inst.RecordEvent(EventWorktreeCreating, spec.VCS.Branch)
	if err := p.addWorktree(spec.BareRoot, worktreePath, spec.VCS.Branch, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		inst.RecordEvent(EventProvisionFailed, errMsg)
		inst.LastError = &errMsg
		p.persistState(store, spec.Name, inst)
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add failed",
			Detail:  err.Error(),
		}
	}
	inst.RecordEvent(EventWorktreeCreated, spec.VCS.Branch)

	p.writeLog(spec.Name, "provision", "Worktree add complete")

	p.hydrate(inst, spec)
	inst.HeadCommit = ResolveHeadCommit(spec.ProjectRoot, spec.Owner)
	p.startIDE(inst, spec)

	if err := p.persistState(store, spec.Name, inst); err != nil {
		return nil, err
	}

	p.writeLog(spec.Name, "provision", "Lifecycle: %s", inst.Status)
	return inst, nil
}

// startIDE allocates a port, starts the IDE adapter, and updates instance state.
// IDE failures are non-fatal — events are emitted but lifecycle stays Ready.
func (p *Provisioner) startIDE(inst *Workspace, spec WorkspaceSpec) {
	if spec.IDE == nil || p.IDEAdapter == nil || p.PortAllocator == nil {
		return
	}

	key := PortKey(spec.Owner, spec.WorktreeName)
	port, err := p.PortAllocator.Allocate(key)
	if err != nil {
		// Create a minimal IDEInstance to record the failure event
		if inst.IDE == nil {
			inst.IDE = &IDEInstance{Name: spec.Name, Adapter: p.IDEAdapter.Name(), Port: 0}
		}
		inst.IDE.RecordEvent(IDEEventFailed, fmt.Sprintf("port allocation: %v", err))
		p.writeLog(spec.Name, "ide", "Port allocation failed: %v", err)
		return
	}

	// Initialise (or re-initialise) the IDEInstance for this adapter + port
	inst.IDE = &IDEInstance{
		Name:    spec.Name,
		Adapter: p.IDEAdapter.Name(),
		Port:    port,
	}

	ctx := IDEContext{
		Owner:        spec.Owner,
		WorktreePath: spec.ProjectRoot,
		WorktreeName: spec.WorktreeName,
		Port:         port,
	}

	inst.IDE.RecordEvent(IDEEventStarted, fmt.Sprintf("port=%d", port))
	if err := p.IDEAdapter.Start(ctx); err != nil {
		inst.IDE.RecordEvent(IDEEventFailed, err.Error())
		p.writeLog(spec.Name, "ide", "Start failed: %v", err)
		return
	}

	inst.IDE.RecordEvent(IDEEventReady, fmt.Sprintf("port=%d", port))
	p.writeLog(spec.Name, "ide", "Started on port %d", port)
}

// stopIDE stops the IDE adapter and releases the port. Best-effort.
// The IDEInstance is preserved (not nil'd) with a StatusPending status and an
// ide_stopped event in the trail. A nil inst.IDE means "IDE was never configured",
// not "IDE was stopped".
func (p *Provisioner) stopIDE(inst *Workspace, spec WorkspaceSpec) {
	if inst.IDE == nil || p.IDEAdapter == nil || p.PortAllocator == nil {
		return
	}

	ctx := IDEContext{
		Owner:        spec.Owner,
		WorktreePath: spec.ProjectRoot,
		WorktreeName: spec.WorktreeName,
		Port:         inst.IDE.Port,
	}

	if err := p.IDEAdapter.Stop(ctx); err != nil {
		p.writeLog(spec.Name, "ide", "Stop failed: %v", err)
	}

	key := PortKey(spec.Owner, spec.WorktreeName)
	if err := p.PortAllocator.Release(key); err != nil {
		p.writeLog(spec.Name, "ide", "Port release failed: %v", err)
	}

	inst.IDE.RecordEvent(IDEEventStopped, fmt.Sprintf("port=%d", inst.IDE.Port))
	p.writeLog(spec.Name, "ide", "Stopped")
}

// healthCheckIDE checks if a running IDE is still healthy, updating status via events.
func (p *Provisioner) healthCheckIDE(inst *Workspace) {
	if inst.IDE == nil || p.IDEAdapter == nil {
		return
	}

	ctx := IDEContext{
		Owner:        inst.Spec.Owner,
		WorktreePath: inst.Spec.ProjectRoot,
		WorktreeName: inst.Spec.WorktreeName,
		Port:         inst.IDE.Port,
	}

	err := p.IDEAdapter.HealthCheck(ctx)
	wasReady := inst.IDE.Status == StatusReady

	if err != nil && wasReady {
		inst.IDE.RecordEvent(IDEEventStopped, "health check failed")
		p.writeLog(inst.Spec.Name, "ide", "Health check failed, marking inactive")
	}
}

// resolveIDEAdapter validates the adapter name from the spec. Returns an error
// for unknown adapter names.
func (p *Provisioner) resolveIDEAdapter(spec WorkspaceSpec) error {
	if spec.IDE == nil {
		return nil
	}
	if p.IDEAdapter == nil {
		return &ProvisionError{
			Code:    ErrSpecInvalid,
			Message: "IDE requested but no adapter configured",
		}
	}
	if spec.IDE.Adapter != p.IDEAdapter.Name() {
		return &ProvisionError{
			Code:    ErrSpecInvalid,
			Message: fmt.Sprintf("unknown IDE adapter %q", spec.IDE.Adapter),
		}
	}
	return nil
}

// hydrate fetches and fast-forward merges matching worktrees so the user lands
// on an up-to-date checkout. A worktree is a candidate if it is the default
// worktree or its branch matches spec.VCS.Branch. Hydration is best-effort:
// fetch failures, dirty worktrees, and diverged branches are logged and skipped
// without blocking provisioning.
func (p *Provisioner) hydrate(inst *Workspace, spec WorkspaceSpec) {
	inst.RecordEvent(EventHydrateStarted, "")
	p.writeLog(spec.Name, "hydrate", "Starting hydration for %s", spec.BareRoot)

	entries, err := ListWorktreeEntries(spec.BareRoot, spec.Owner)
	if err != nil {
		p.writeLog(spec.Name, "hydrate", "Failed to list worktrees: %v", err)
		inst.RecordEvent(EventHydrateSkipped, fmt.Sprintf("worktree list failed: %v", err))
		return
	}

	for _, entry := range entries {
		// Filter: only hydrate the default worktree or worktrees on the requested branch
		isDefault := filepath.Base(entry.Path) == "default"
		branchMatch := entry.Branch == spec.VCS.Branch

		if !isDefault && !branchMatch {
			continue
		}

		targetBranch := entry.Branch
		if targetBranch == "" {
			// Detached HEAD or unknown branch — skip
			p.writeLog(spec.Name, "hydrate", "Skipping %s: no branch", entry.Path)
			inst.RecordEvent(EventHydrateSkipped, fmt.Sprintf("%s: detached HEAD", filepath.Base(entry.Path)))
			continue
		}

		// Check for dirty worktree before pulling
		dirty, dirtyErr := IsWorktreeDirty(entry.Path, spec.Owner)
		if dirtyErr != nil {
			p.writeLog(spec.Name, "hydrate", "Dirty check failed for %s: %v", entry.Path, dirtyErr)
			inst.RecordEvent(EventHydrateSkipped, fmt.Sprintf("%s: dirty check failed: %v", filepath.Base(entry.Path), dirtyErr))
			continue
		}
		if dirty {
			p.writeLog(spec.Name, "hydrate", "Skipping %s: uncommitted changes", entry.Path)
			inst.RecordEvent(EventHydrateSkipped, fmt.Sprintf("%s: uncommitted changes", filepath.Base(entry.Path)))
			continue
		}

		// Pull with fast-forward only (fetch + merge in one step).
		// Using pull from the worktree context ensures proper ref resolution
		// even when the bare clone lacks a fetch refspec.
		pullErr := p.ffPull(entry.Path, targetBranch, spec.Owner)
		if pullErr != nil {
			errStr := pullErr.Error()
			if strings.Contains(errStr, "Not possible to fast-forward") || strings.Contains(errStr, "fatal:") {
				p.writeLog(spec.Name, "hydrate", "FF pull failed for %s: %v", entry.Path, pullErr)
				inst.RecordEvent(EventHydrateSkipped, fmt.Sprintf("%s: branch diverged, ff-only not possible", filepath.Base(entry.Path)))
			} else {
				p.writeLog(spec.Name, "hydrate", "Pull failed for %s: %v", entry.Path, pullErr)
				inst.RecordEvent(EventHydrateSkipped, fmt.Sprintf("%s: pull failed: %v", filepath.Base(entry.Path), pullErr))
			}
			continue
		}

		p.writeLog(spec.Name, "hydrate", "Hydrated %s (branch: %s)", entry.Path, targetBranch)
		inst.RecordEvent(EventHydrateCompleted, targetBranch)
	}
}

// ffPull runs git -C <worktreePath> pull --ff-only origin <branch> as owner.
// This combines fetch and fast-forward merge in one step, which works correctly
// in worktrees backed by a bare clone (where remote tracking refs may not be
// configured).
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

// DeprovisionResult holds the outcome of a deprovision operation.
type DeprovisionResult struct {
	Removed []string `json:"removed"` // workspace names removed
	Message string   `json:"message"`
}

// Deprovision removes a single non-default worktree with dirty guards.
func (p *Provisioner) Deprovision(store StateStore, name string, force bool) (*DeprovisionResult, error) {
	// 1. Load instance from state store by name
	instances, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	inst, ok := instances[name]
	if !ok {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("workspace '%s' not found", name),
		}
	}

	// 2. Guard: cannot delete default worktree
	if inst.Spec.IsDefault {
		return nil, &ProvisionError{
			Code:    ErrCannotDeleteDefault,
			Message: "Cannot delete default worktree. Use --all to remove the entire workspace including the bare clone.",
		}
	}

	// 3. Guard: check for uncommitted changes (unless --force)
	if !force {
		dirty, dirtyErr := IsWorktreeDirty(inst.Spec.ProjectRoot, inst.Spec.Owner)
		if dirtyErr == nil && dirty {
			return nil, &ProvisionError{
				Code:    ErrWorktreeDirty,
				Message: fmt.Sprintf("Worktree '%s' has uncommitted changes. Use --force to delete.", inst.Spec.WorktreeName),
			}
		}
	}

	// 4. Stop IDE if running
	p.stopIDE(inst, inst.Spec)

	// 5. Remove worktree via git
	if err := p.removeWorktree(inst.Spec.BareRoot, inst.Spec.ProjectRoot, inst.Spec.Owner, force); err != nil {
		return nil, fmt.Errorf("git worktree remove: %w", err)
	}

	// 6. Prune stale worktree metadata
	p.pruneWorktrees(inst.Spec.BareRoot, inst.Spec.Owner)

	// 7. Remove state entry
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

	p.writeLog(name, "deprovision", "Worktree removed")

	suffix := ""
	if force {
		suffix = " (forced)"
	}
	return &DeprovisionResult{
		Removed: []string{name},
		Message: fmt.Sprintf("Workspace '%s' removed%s.", name, suffix),
	}, nil
}

// DeprovisionAll removes all worktrees, the bare clone, and the repo container for a workspace.
func (p *Provisioner) DeprovisionAll(store StateStore, repoName string, force bool) (*DeprovisionResult, error) {
	// 1. Find all instances matching the repo (by repo_root prefix)
	instances, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	var matching []*Workspace
	var matchingNames []string
	var repoRoot, bareRoot string

	for iname, inst := range instances {
		// Match by exact repoName (default workspace) or by prefix "repoName/"
		if iname == repoName || strings.HasPrefix(iname, repoName+"/") {
			matching = append(matching, inst)
			matchingNames = append(matchingNames, iname)
			if repoRoot == "" {
				repoRoot = inst.Spec.RepoRoot
				bareRoot = inst.Spec.BareRoot
			}
		}
	}

	if len(matching) == 0 {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("no workspaces found for '%s'", repoName),
		}
	}

	// 2. Guard: check all worktrees for uncommitted changes (unless --force)
	if !force {
		for _, inst := range matching {
			dirty, dirtyErr := IsWorktreeDirty(inst.Spec.ProjectRoot, inst.Spec.Owner)
			if dirtyErr == nil && dirty {
				return nil, &ProvisionError{
					Code:    ErrWorktreeDirty,
					Message: fmt.Sprintf("Worktree '%s' has uncommitted changes. Use --force to delete.", inst.Spec.WorktreeName),
				}
			}
		}
	}

	// 3. Stop all IDE adapters before removing worktrees
	for _, inst := range matching {
		p.stopIDE(inst, inst.Spec)
	}

	// 4. Remove all worktrees via git worktree remove (non-default first, default last)
	for _, inst := range matching {
		if inst.Spec.IsDefault {
			continue
		}
		_ = p.removeWorktree(inst.Spec.BareRoot, inst.Spec.ProjectRoot, inst.Spec.Owner, force)
	}
	for _, inst := range matching {
		if !inst.Spec.IsDefault {
			continue
		}
		_ = p.removeWorktree(inst.Spec.BareRoot, inst.Spec.ProjectRoot, inst.Spec.Owner, force)
	}

	// 4. Remove the bare clone: rm -rf <bare_root>
	if bareRoot != "" {
		os.RemoveAll(bareRoot)
	}

	// 5. Remove the repo container: rm -rf <repo_root> (if empty or we're cleaning up)
	if repoRoot != "" {
		os.RemoveAll(repoRoot)
	}

	// 6. Remove all state entries for this repo
	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		for _, iname := range matchingNames {
			delete(instances, iname)
		}
		return store.Save(instances)
	}); err != nil {
		return nil, fmt.Errorf("remove state entries: %w", err)
	}

	// Collect worktree names for the result
	var wtNames []string
	for _, inst := range matching {
		wtNames = append(wtNames, inst.Spec.WorktreeName)
	}

	p.writeLog(repoName, "deprovision", "Full removal: worktrees=%v, bare=%s", wtNames, bareRoot)

	return &DeprovisionResult{
		Removed: matchingNames,
		Message: fmt.Sprintf("Workspace '%s' fully removed.", repoName),
	}, nil
}

// Prune removes all clean non-default worktrees for a given workspace (repo).
// Dirty worktrees are skipped with a reason. The default worktree is never pruned.
func (p *Provisioner) Prune(store StateStore, repoName string) (*PruneResult, error) {
	// 1. Load all instances from state store
	instances, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("load state: %w", err)
	}

	// 2. Filter to instances matching the repo (by name or name prefix "repoName/")
	var matching []*Workspace
	var matchingNames []string
	var bareRoot string

	for iname, inst := range instances {
		if iname == repoName || strings.HasPrefix(iname, repoName+"/") {
			matching = append(matching, inst)
			matchingNames = append(matchingNames, iname)
			if bareRoot == "" {
				bareRoot = inst.Spec.BareRoot
			}
		}
	}

	if len(matching) == 0 {
		return nil, &ProvisionError{
			Code:    ErrNotFound,
			Message: fmt.Sprintf("no workspaces found for '%s'", repoName),
		}
	}

	result := &PruneResult{
		Pruned:  []string{},
		Skipped: []PruneSkipped{},
	}

	// 3. For each non-default worktree, check dirty and remove or skip
	var namesToRemove []string
	for i, inst := range matching {
		name := matchingNames[i]

		// Skip the default worktree
		if inst.Spec.IsDefault {
			continue
		}

		// Check if dirty
		dirty, dirtyErr := IsWorktreeDirty(inst.Spec.ProjectRoot, inst.Spec.Owner)
		if dirtyErr == nil && dirty {
			result.Skipped = append(result.Skipped, PruneSkipped{
				Name:   name,
				Reason: "uncommitted changes",
			})
			continue
		}

		// Stop IDE before removing
		p.stopIDE(inst, inst.Spec)

		// Clean: remove worktree via git
		if err := p.removeWorktree(inst.Spec.BareRoot, inst.Spec.ProjectRoot, inst.Spec.Owner, false); err != nil {
			result.Skipped = append(result.Skipped, PruneSkipped{
				Name:   name,
				Reason: fmt.Sprintf("remove failed: %v", err),
			})
			continue
		}

		result.Pruned = append(result.Pruned, name)
		namesToRemove = append(namesToRemove, name)
		p.writeLog(name, "prune", "Worktree removed")
	}

	// 4. Run git worktree prune on bare root
	if bareRoot != "" {
		owner := ""
		if len(matching) > 0 {
			owner = matching[0].Spec.Owner
		}
		p.pruneWorktrees(bareRoot, owner)
	}

	// 5. Remove state entries for pruned worktrees
	if len(namesToRemove) > 0 {
		if err := store.WithLock(func() error {
			instances, err := store.Load()
			if err != nil {
				return err
			}
			for _, name := range namesToRemove {
				delete(instances, name)
			}
			return store.Save(instances)
		}); err != nil {
			return nil, fmt.Errorf("remove state entries: %w", err)
		}
	}

	// 6. Build summary message
	prunedCount := len(result.Pruned)
	skippedCount := len(result.Skipped)

	// Check if there were no non-default worktrees at all
	hasNonDefault := false
	for _, inst := range matching {
		if !inst.Spec.IsDefault {
			hasNonDefault = true
			break
		}
	}

	if !hasNonDefault {
		result.Message = "No non-default worktrees to prune."
	} else if skippedCount == 0 {
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

// bareClone runs git clone --bare as the spec owner.
func (p *Provisioner) bareClone(spec WorkspaceSpec) error {
	cloneCmd := fmt.Sprintf("git clone --bare %s %s", spec.VCS.CloneURL, spec.BareRoot)

	var cmd *exec.Cmd
	if spec.Owner != "" && spec.Owner != currentUser() {
		cmd = exec.Command("su", "-", spec.Owner, "-c", cloneCmd)
	} else {
		cmd = exec.Command("git", "clone", "--bare", spec.VCS.CloneURL, spec.BareRoot)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// addWorktree runs git -C <bareRoot> worktree add [-f] <path> <branch> as the owner.
// The -f flag allows checking out a branch already used by another worktree.
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
// Worktrees have .git as a file; traditional clones have .git as a directory.
// Either form counts as existing.
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

func (p *Provisioner) persistState(store StateStore, name string, inst *Workspace) error {
	return store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		instances[name] = inst
		return store.Save(instances)
	})
}

func (p *Provisioner) writeLog(name, phase, format string, args ...interface{}) {
	if p.LogDir == "" {
		return
	}
	os.MkdirAll(p.LogDir, 0755)
	logPath := filepath.Join(p.LogDir, name+".log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(f, "[%s] [%s] %s\n", ts, phase, msg)
}

func validateSpec(spec WorkspaceSpec) error {
	var missing []string
	if spec.Name == "" {
		missing = append(missing, "name")
	}
	// VCS.CloneURL and VCS.Branch are required only when Template is absent
	if spec.VCS.CloneURL == "" && spec.Template == nil {
		missing = append(missing, "vcs.clone_url")
	}
	if spec.VCS.Branch == "" && spec.Template == nil {
		missing = append(missing, "vcs.branch")
	}
	if spec.ProjectRoot == "" {
		missing = append(missing, "project_root")
	}
	if spec.RepoRoot == "" {
		missing = append(missing, "repo_root")
	}
	if spec.BareRoot == "" {
		missing = append(missing, "bare_root")
	}
	if spec.WorktreeName == "" {
		missing = append(missing, "worktree_name")
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

	// Validate spec.Name for custom name safety.
	if err := validateName(spec.Name); err != nil {
		return err
	}

	// Template-specific validation
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

// validateName checks that a workspace name is safe for state file keying,
// filesystem operations, and display. Custom names (not derived from repo slugs)
// must satisfy these constraints:
//   - Length <= 128 characters
//   - At most one "/" separator (worktree notation: "name/branch")
//   - No path traversal segments ("." or "..")
//   - No null bytes or shell/filesystem-unsafe characters
func validateName(name string) error {
	// Length check
	if len(name) > 128 {
		return &ProvisionError{
			Code:    ErrSpecInvalid,
			Message: "name too long",
			Detail:  fmt.Sprintf("name must be <= 128 characters, got %d", len(name)),
		}
	}

	// At most one "/" (worktree notation: "repo/branch")
	segments := strings.Split(name, "/")
	if len(segments) > 2 {
		return &ProvisionError{
			Code:    ErrSpecInvalid,
			Message: "name contains too many path segments",
			Detail:  fmt.Sprintf("at most one '/' allowed (worktree notation), got %d segments", len(segments)),
		}
	}

	// No path traversal or empty segments
	for _, seg := range segments {
		if seg == "" {
			return &ProvisionError{
				Code:    ErrSpecInvalid,
				Message: "name contains empty segment",
				Detail:  "name segments must be non-empty (no leading, trailing, or consecutive '/')",
			}
		}
		if seg == "." || seg == ".." {
			return &ProvisionError{
				Code:    ErrSpecInvalid,
				Message: "name contains path traversal",
				Detail:  fmt.Sprintf("segment %q is not allowed", seg),
			}
		}
	}

	// No null bytes or shell/filesystem-unsafe characters
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

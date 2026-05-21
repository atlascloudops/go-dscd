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
	StorePath string // path to state.json
	LogDir    string // /opt/dsc/var/dscd/logs/
}

// Provision creates a workspace using dual-mode provisioning:
//   - If IsDefault and no bare clone exists: bare clone + default worktree
//   - If bare clone exists (or is created): add worktree from existing bare
func (p *Provisioner) Provision(store StateStore, spec WorkspaceSpec) (*WorkspaceInstance, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}

	// Idempotency check — if worktree already exists, return ready
	if worktreeExists(spec.ProjectRoot) {
		return p.returnIdempotent(store, spec)
	}

	bareExists := dirExists(spec.BareRoot)

	if spec.IsDefault && !bareExists {
		return p.provisionBareCloneAndDefault(store, spec)
	}

	if !bareExists {
		// Non-default worktree requested but no bare clone — create bare clone first
		if err := p.bareClone(spec); err != nil {
			return nil, err
		}
	}

	return p.provisionWorktree(store, spec)
}

// returnIdempotent handles the case where the worktree already exists.
func (p *Provisioner) returnIdempotent(store StateStore, spec WorkspaceSpec) (*WorkspaceInstance, error) {
	now := time.Now().UTC()
	p.writeLog(spec.Name, "provision", "Worktree already exists at %s, skipping", spec.ProjectRoot)
	inst := &WorkspaceInstance{
		Spec:            spec,
		State:           StateReady,
		CloneExists:     true,
		CredentialHost:  spec.VCS.Host,
		CredentialFresh: p.checkCredentialFresh(spec),
		HeadCommit:      ResolveHeadCommit(spec.ProjectRoot, spec.Owner),
		ProvisionedAt:   &now,
	}
	inst.DeriveStatus()
	if err := store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		instances[spec.Name] = inst
		return store.Save(instances)
	}); err != nil {
		return nil, err
	}
	p.writeLog(spec.Name, "provision", "State: ready (idempotent)")
	return inst, nil
}

// provisionBareCloneAndDefault creates a bare clone and the default worktree.
func (p *Provisioner) provisionBareCloneAndDefault(store StateStore, spec WorkspaceSpec) (*WorkspaceInstance, error) {
	now := time.Now().UTC()
	p.writeLog(spec.Name, "provision", "Bare-cloning %s into %s", spec.VCS.CloneURL, spec.BareRoot)

	inst := &WorkspaceInstance{
		Spec:           spec,
		State:          StateProvisioning,
		CredentialHost: spec.VCS.Host,
		ProvisionedAt:  &now,
	}

	// 1. mkdir -p <repo_root>
	if err := os.MkdirAll(spec.RepoRoot, 0755); err != nil {
		return nil, fmt.Errorf("create repo root: %w", err)
	}

	// 2. git clone --bare <clone_url> <bare_root>
	if err := p.bareClone(spec); err != nil {
		errMsg := fmt.Sprintf("git clone --bare failed: %v", err)
		inst.State = StateError
		inst.LastError = &errMsg
		inst.CloneExists = false
		inst.DeriveStatus()
		p.persistState(store, spec.Name, inst)
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git clone --bare failed",
			Detail:  err.Error(),
		}
	}

	// 3. Resolve default branch
	defaultBranch, err := resolveDefaultBranch(spec.BareRoot, spec.Owner)
	if err != nil {
		p.writeLog(spec.Name, "provision", "Could not resolve default branch, falling back to main: %v", err)
		defaultBranch = "main"
	}
	p.writeLog(spec.Name, "provision", "Default branch resolved: %s", defaultBranch)

	// 4. git -C <bare_root> worktree add ../default <default_branch>
	worktreePath := filepath.Join(spec.RepoRoot, "default")
	if err := p.addWorktree(spec.BareRoot, worktreePath, defaultBranch, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		inst.State = StateError
		inst.LastError = &errMsg
		inst.CloneExists = false
		inst.DeriveStatus()
		p.persistState(store, spec.Name, inst)
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add (default) failed",
			Detail:  err.Error(),
		}
	}

	p.writeLog(spec.Name, "provision", "Bare clone + default worktree complete")

	inst.State = StateReady
	inst.CloneExists = true
	inst.CredentialFresh = p.checkCredentialFresh(spec)
	inst.HeadCommit = ResolveHeadCommit(spec.ProjectRoot, spec.Owner)
	inst.DeriveStatus()

	if err := p.persistState(store, spec.Name, inst); err != nil {
		return nil, err
	}

	p.writeLog(spec.Name, "provision", "State: ready")
	return inst, nil
}

// provisionWorktree adds a worktree from an existing bare clone.
func (p *Provisioner) provisionWorktree(store StateStore, spec WorkspaceSpec) (*WorkspaceInstance, error) {
	now := time.Now().UTC()
	p.writeLog(spec.Name, "provision", "Adding worktree %s (branch: %s)", spec.WorktreeName, spec.VCS.Branch)

	inst := &WorkspaceInstance{
		Spec:           spec,
		State:          StateProvisioning,
		CredentialHost: spec.VCS.Host,
		ProvisionedAt:  &now,
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
	if err := p.addWorktree(spec.BareRoot, worktreePath, spec.VCS.Branch, spec.Owner); err != nil {
		errMsg := fmt.Sprintf("git worktree add failed: %v", err)
		inst.State = StateError
		inst.LastError = &errMsg
		inst.CloneExists = false
		inst.DeriveStatus()
		p.persistState(store, spec.Name, inst)
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git worktree add failed",
			Detail:  err.Error(),
		}
	}

	p.writeLog(spec.Name, "provision", "Worktree add complete")

	inst.State = StateReady
	inst.CloneExists = true
	inst.CredentialFresh = p.checkCredentialFresh(spec)
	inst.HeadCommit = ResolveHeadCommit(spec.ProjectRoot, spec.Owner)
	inst.DeriveStatus()

	if err := p.persistState(store, spec.Name, inst); err != nil {
		return nil, err
	}

	p.writeLog(spec.Name, "provision", "State: ready")
	return inst, nil
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

	// 4. Remove worktree via git
	if err := p.removeWorktree(inst.Spec.BareRoot, inst.Spec.ProjectRoot, inst.Spec.Owner, force); err != nil {
		return nil, fmt.Errorf("git worktree remove: %w", err)
	}

	// 5. Prune stale worktree metadata
	p.pruneWorktrees(inst.Spec.BareRoot, inst.Spec.Owner)

	// 6. Remove state entry
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

	var matching []*WorkspaceInstance
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

	// 3. Remove all worktrees via git worktree remove (non-default first, default last)
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
	var matching []*WorkspaceInstance
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

// addWorktree runs git -C <bareRoot> worktree add <path> <branch> as the owner.
func (p *Provisioner) addWorktree(bareRoot, worktreePath, branch, owner string) error {
	wtCmd := fmt.Sprintf("git -C %s worktree add %s %s", bareRoot, worktreePath, branch)

	var cmd *exec.Cmd
	if owner != "" && owner != currentUser() {
		cmd = exec.Command("su", "-", owner, "-c", wtCmd)
	} else {
		cmd = exec.Command("git", "-C", bareRoot, "worktree", "add", worktreePath, branch)
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

func (p *Provisioner) persistState(store StateStore, name string, inst *WorkspaceInstance) error {
	return store.WithLock(func() error {
		instances, err := store.Load()
		if err != nil {
			return err
		}
		instances[name] = inst
		return store.Save(instances)
	})
}

func (p *Provisioner) checkCredentialFresh(spec WorkspaceSpec) bool {
	credPath := filepath.Join("/home", spec.Owner, ".config/dsc/credentials/git-credentials")
	data, err := os.ReadFile(credPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), spec.VCS.Host)
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
	if spec.VCS.CloneURL == "" {
		missing = append(missing, "vcs.clone_url")
	}
	if spec.VCS.Branch == "" {
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

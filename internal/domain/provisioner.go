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

func (p *Provisioner) Provision(store StateStore, spec WorkspaceSpec) (*WorkspaceInstance, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	// Idempotency check — if clone already exists, return ready
	gitDir := filepath.Join(spec.ProjectRoot, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		p.writeLog(spec.Name, "provision", "Clone already exists at %s, skipping", spec.ProjectRoot)
		inst := &WorkspaceInstance{
			Spec:            spec,
			State:           StateReady,
			CloneExists:     true,
			CredentialHost:  spec.VCS.Host,
			CredentialFresh: p.checkCredentialFresh(spec),
			ProvisionedAt:   &now,
		}
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

	p.writeLog(spec.Name, "provision", "Cloning %s (branch: %s)", spec.VCS.CloneURL, spec.VCS.Branch)

	// Set state to provisioning
	inst := &WorkspaceInstance{
		Spec:           spec,
		State:          StateProvisioning,
		CredentialHost: spec.VCS.Host,
		ProvisionedAt:  &now,
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(spec.ProjectRoot), 0755); err != nil {
		return nil, fmt.Errorf("create parent dir: %w", err)
	}

	// Clone repository as the owner user
	cloneCmd := fmt.Sprintf("git clone --branch %s %s %s",
		spec.VCS.Branch, spec.VCS.CloneURL, spec.ProjectRoot)

	var cmd *exec.Cmd
	if spec.Owner != "" && spec.Owner != currentUser() {
		cmd = exec.Command("su", "-", spec.Owner, "-c", cloneCmd)
	} else {
		cmd = exec.Command("git", "clone", "--branch", spec.VCS.Branch, spec.VCS.CloneURL, spec.ProjectRoot)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := fmt.Sprintf("git clone failed: %s", strings.TrimSpace(string(output)))
		inst.State = StateError
		inst.LastError = &errMsg
		inst.CloneExists = false

		// Persist error state
		_ = store.WithLock(func() error {
			instances, loadErr := store.Load()
			if loadErr != nil {
				return loadErr
			}
			instances[spec.Name] = inst
			return store.Save(instances)
		})
		p.writeLog(spec.Name, "error", "%s", errMsg)
		return inst, &ProvisionError{
			Code:    ErrCloneFailed,
			Message: "git clone failed",
			Detail:  strings.TrimSpace(string(output)),
		}
	}

	p.writeLog(spec.Name, "provision", "Clone complete")

	inst.State = StateReady
	inst.CloneExists = true
	inst.CredentialFresh = p.checkCredentialFresh(spec)

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

	p.writeLog(spec.Name, "provision", "State: ready")
	return inst, nil
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

package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/atlascloudops/go-dscd/internal/store"
	"github.com/spf13/cobra"
)

const (
	defaultStatePath   = "/var/lib/dscd/state.json"
	defaultPortFile    = "/var/lib/dscd/ports.json"
	defaultActivityLog = domain.DefaultActivityLogPath
)

var jsonOutput bool

func NewRootCommand(version string) *cobra.Command {
	var statePath string
	var activityLogPath string
	var logLevel string
	var workspaceRoot string

	root := &cobra.Command{
		Use:   "dscd",
		Short: "Daemon for workspace lifecycle management",
		Long:  "dscd manages workspace provisioning, sync, and status on a pod.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	root.PersistentFlags().StringVar(&statePath, "state-path", defaultStatePath, "path to state file")
	root.PersistentFlags().StringVar(&activityLogPath, "activity-log", defaultActivityLog, "path to activity log file")
	root.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")
	root.PersistentFlags().StringVar(&workspaceRoot, "workspace-root", "", "workspace root directory (default: ~/code, override with DSCD_WORKSPACE_ROOT)")

	root.Version = version
	root.SetVersionTemplate(fmt.Sprintf("dscd v%s\n", version))

	// Lazy init store so flags are parsed first
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {}

	workspace := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Wire subcommands with a factory that resolves the store lazily
	storeFactory := func() *store.FileStore {
		return store.NewFileStore(statePath)
	}

	// Lazy init activity log so flags are parsed first
	al := &lazyActivityLog{factory: func() *domain.ActivityLog {
		return domain.NewActivityLog(activityLogPath)
	}}

	// We need to add commands after flag parsing, so use PersistentPreRun
	// Actually cobra parses flags before RunE, so factory works fine
	fs := &lazyStore{factory: storeFactory}

	workspace.AddCommand(
		newWorkspaceProvisionCmd(fs, al.get(), &workspaceRoot),
		newWorkspaceDeprovisionCmd(fs, al.get()),
		newWorkspacePruneCmd(fs, al.get()),
		newWorkspaceListCmd(fs),
		newWorkspaceInspectCmd(fs),
		newWorkspaceSyncCmd(fs, al.get()),
		newWorkspaceHydrateCmd(fs, al.get(), &workspaceRoot),
		newWorkspaceBootCmd(fs, al.get(), &workspaceRoot),
		newWorkspaceWorktreeCmd(fs, al.get()),
		newWorkspaceIDECmd(fs, al.get()),
	)

	credentials := &cobra.Command{
		Use:   "credentials",
		Short: "Manage pod credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	gitCreds := &cobra.Command{
		Use:   "git",
		Short: "Manage git credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	gitCreds.AddCommand(
		newCredentialsGitListCmd(),
		newCredentialsGitWriteCmd(fs, al.get()),
	)
	ssoCreds := &cobra.Command{
		Use:   "sso",
		Short: "Manage SSO credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	ssoCreds.AddCommand(
		newCredentialsSsoStatusCmd(),
		newCredentialsSsoWriteCmd(fs, al.get()),
	)
	credentials.AddCommand(gitCreds, ssoCreds)

	shell := &cobra.Command{
		Use:   "shell",
		Short: "Manage shell environment hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	shellEnv := &cobra.Command{
		Use:   "env",
		Short: "Manage shell environment variables",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	shellEnv.AddCommand(newShellEnvSetCmd())
	shell.AddCommand(newShellInstallCmd(), shellEnv)

	handshake := &cobra.Command{
		Use:   "handshake",
		Short: "Composed credential and environment sync",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	handshake.AddCommand(
		newHandshakeStatusCmd(),
		newHandshakeSyncCmd(fs, al.get()),
	)

	root.AddCommand(
		workspace,
		credentials,
		shell,
		handshake,
		newStatusCmd(fs, version, &statePath),
		newEventsCmd(func() *domain.ActivityLog {
			return domain.NewActivityLog(activityLogPath)
		}, &activityLogPath),
	)
	return root
}

// lazyActivityLog wraps ActivityLog creation so flag values are resolved at call time.
type lazyActivityLog struct {
	factory func() *domain.ActivityLog
	inst    *domain.ActivityLog
}

func (l *lazyActivityLog) get() *domain.ActivityLog {
	if l.inst == nil {
		l.inst = l.factory()
	}
	return l.inst
}

// lazyStore wraps store creation so flag values are resolved at call time, not registration time
type lazyStore struct {
	factory func() *store.FileStore
	inst    *store.FileStore
}

func (l *lazyStore) get() *store.FileStore {
	if l.inst == nil {
		l.inst = l.factory()
	}
	return l.inst
}

func (l *lazyStore) Load() (map[string]*domain.Workspace, error) {
	return l.get().Load()
}

func (l *lazyStore) Save(instances map[string]*domain.Workspace) error {
	return l.get().Save(instances)
}

func (l *lazyStore) LoadState() (*domain.DaemonState, error) {
	return l.get().LoadState()
}

func (l *lazyStore) SaveState(state *domain.DaemonState) error {
	return l.get().SaveState(state)
}

func (l *lazyStore) WithLock(fn func() error) error {
	return l.get().WithLock(fn)
}

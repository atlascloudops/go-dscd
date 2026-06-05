package cli

import (
	"fmt"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/atlascloudops/go-dscd/internal/store"
	"github.com/spf13/cobra"
)

const (
	defaultStatePath = "/opt/dsc/var/dscd/state.json"
	defaultLogDir    = "/opt/dsc/var/dscd/logs"
	defaultPortFile  = "/opt/dsc/var/dscd/ports.json"
)

var jsonOutput bool

func NewRootCommand(version string) *cobra.Command {
	var statePath string
	var logDir string

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
	root.PersistentFlags().StringVar(&logDir, "log-dir", defaultLogDir, "path to log directory")

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

	// We need to add commands after flag parsing, so use PersistentPreRun
	// Actually cobra parses flags before RunE, so factory works fine
	fs := &lazyStore{factory: storeFactory}

	workspace.AddCommand(
		newWorkspaceProvisionCmd(fs, logDir),
		newWorkspaceDeprovisionCmd(fs, logDir),
		newWorkspacePruneCmd(fs, logDir),
		newWorkspaceListCmd(fs),
		newWorkspaceInspectCmd(fs),
		newWorkspaceSyncCmd(fs, logDir),
		newWorkspaceLogsCmd(fs, logDir),
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
		newCredentialsGitWriteCmd(),
	)
	credentials.AddCommand(gitCreds)

	root.AddCommand(workspace, credentials, newStatusCmd(fs, version, &statePath))
	return root
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

func (l *lazyStore) Load() (map[string]*domain.WorkspaceInstance, error) {
	return l.get().Load()
}

func (l *lazyStore) Save(instances map[string]*domain.WorkspaceInstance) error {
	return l.get().Save(instances)
}

func (l *lazyStore) WithLock(fn func() error) error {
	return l.get().WithLock(fn)
}

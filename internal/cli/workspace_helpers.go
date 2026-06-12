package cli

import (
	"log"

	"github.com/atlascloudops/go-dscd/internal/infrastructure"
)

// resolveOwnerFromPodConfig reads the pod config file to determine the linux
// username that owns workspaces. Returns empty string on failure (callers fall
// back to os.UserHomeDir via ResolveWorkspaceRoot("")).
func resolveOwnerFromPodConfig() string {
	owner, err := infrastructure.ReadPodOwner("")
	if err != nil {
		log.Printf("warning: unable to read pod config for workspace owner: %v", err)
		return ""
	}
	return owner
}

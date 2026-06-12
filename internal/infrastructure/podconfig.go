package infrastructure

import (
	"encoding/json"
	"os"
)

// DefaultPodConfigPath is the standard location for the pod configuration file,
// written by cloud-init before dscd.service starts.
const DefaultPodConfigPath = "/opt/dsc/etc/pod-config.json"

// podConfig represents the relevant fields of the pod configuration file.
type podConfig struct {
	Pod struct {
		LinuxUsername string `json:"linux_username"`
		Owner         string `json:"owner"`
	} `json:"pod"`
}

// ReadPodOwner reads the pod config file and extracts the linux username.
// Resolution order: linux_username > owner > fallback to empty string.
// Returns ("", nil) if the file is missing or the fields are empty.
func ReadPodOwner(path string) (string, error) {
	if path == "" {
		path = DefaultPodConfigPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var cfg podConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", err
	}

	if cfg.Pod.LinuxUsername != "" {
		return cfg.Pod.LinuxUsername, nil
	}
	if cfg.Pod.Owner != "" {
		return cfg.Pod.Owner, nil
	}
	return "", nil
}

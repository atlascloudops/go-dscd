package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newWorkspaceLogsCmd(store domain.StateStore, logDir string) *cobra.Command {
	var lines int
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show provisioning logs for a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Verify workspace exists in state
			instances, err := store.Load()
			if err == nil {
				if _, ok := instances[name]; !ok {
					resp := domain.ErrorResponse("workspace.logs", domain.ErrorInfo{
						Code:    domain.ErrNotFound,
						Message: fmt.Sprintf("workspace %q not found", name),
					})
					return outputResponse(resp, 1)
				}
			}

			logPath := filepath.Join(logDir, name+".log")
			if _, err := os.Stat(logPath); os.IsNotExist(err) {
				fmt.Printf("No log file for workspace %q (not yet provisioned)\n", name)
				return nil
			}

			if err := tailFile(logPath, lines); err != nil {
				return err
			}

			if follow {
				return followFile(logPath)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 50, "number of lines to show")
	cmd.Flags().BoolVar(&follow, "follow", false, "stream new lines as they are written")
	return cmd
}

func tailFile(path string, n int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var allLines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	start := 0
	if len(allLines) > n {
		start = len(allLines) - n
	}
	for _, line := range allLines[start:] {
		fmt.Println(line)
	}
	return nil
}

func followFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Seek to end
	f.Seek(0, io.SeekEnd)

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		fmt.Print(line)
	}
}

package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/atlascloudops/go-dscd/internal/domain"
	"github.com/spf13/cobra"
)

func newEventsCmd(activityLogFactory func() *domain.ActivityLog, activityLogPath *string) *cobra.Command {
	var (
		scope    string
		kind     string
		since    string
		follow   bool
		lines    int
	)

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Show daemon activity events",
		Long:  "Display chronological cross-aggregate events from the daemon activity log.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Build filter from flags.
			filter := domain.ActivityLogFilter{}

			if scope != "" {
				parts := strings.SplitN(scope, ":", 2)
				if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
					return fmt.Errorf("invalid --scope %q: expected format kind:name", scope)
				}
				filter.ScopeKind = parts[0]
				filter.ScopeName = parts[1]
			}

			if kind != "" {
				if scope != "" {
					return fmt.Errorf("--scope and --kind are mutually exclusive")
				}
				filter.ScopeKind = kind
			}

			if since != "" {
				dur, err := time.ParseDuration(since)
				if err != nil {
					return fmt.Errorf("invalid --since %q: %w", since, err)
				}
				filter.Since = time.Now().Add(-dur)
			}

			if follow {
				return followEvents(*activityLogPath, filter, lines)
			}

			al := activityLogFactory()
			records, err := al.Read(filter)
			if err != nil {
				return err
			}

			// Apply --lines limit (show the last N events).
			if lines > 0 && len(records) > lines {
				records = records[len(records)-lines:]
			}

			if jsonOutput {
				return outputEventsJSON(records)
			}
			return outputEventsTable(records)
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "", "filter by exact scope (e.g. workspace:infra)")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by scope kind (e.g. workspace, ide, credentials)")
	cmd.Flags().StringVar(&since, "since", "", "show events from the last duration (e.g. 1h, 30m)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "tail the activity log for new events")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "number of recent events to show")

	return cmd
}

// outputEventsTable writes events in a human-readable column-aligned table.
func outputEventsTable(records []domain.EventRecord) error {
	if len(records) == 0 {
		fmt.Println("No events found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIMESTAMP\tSCOPE\tEVENT\tDETAIL")
	for _, r := range records {
		ts := r.Timestamp.UTC().Format(time.RFC3339)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ts, r.Scope.String(), r.Event, r.Detail)
	}
	return w.Flush()
}

// outputEventsJSON writes events as a JSON array to stdout.
func outputEventsJSON(records []domain.EventRecord) error {
	if records == nil {
		records = []domain.EventRecord{}
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// followEvents tails the activity log file for new events, printing each
// new matching event as it is appended. It first shows the last N existing
// events (controlled by lines), then watches for new ones.
func followEvents(path string, filter domain.ActivityLogFilter, lines int) error {
	// Read existing events first to show recent history.
	al := domain.NewActivityLog(path)
	existing, err := al.Read(filter)
	if err != nil {
		return err
	}

	if lines > 0 && len(existing) > lines {
		existing = existing[len(existing)-lines:]
	}

	if jsonOutput {
		// In follow mode with JSON, output NDJSON (one object per line).
		for _, r := range existing {
			if err := outputEventNDJSON(r); err != nil {
				return err
			}
		}
	} else {
		if len(existing) > 0 {
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TIMESTAMP\tSCOPE\tEVENT\tDETAIL")
			for _, r := range existing {
				ts := r.Timestamp.UTC().Format(time.RFC3339)
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ts, r.Scope.String(), r.Event, r.Detail)
			}
			w.Flush()
		}
	}

	// Now tail the file for new lines.
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File does not exist yet; create and wait.
			f, err = waitForFile(path)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("follow: open %s: %w", path, err)
		}
	}
	defer f.Close()

	// Seek to end of file so we only get new events.
	if _, err := f.Seek(0, 2); err != nil {
		return fmt.Errorf("follow: seek: %w", err)
	}

	scanner := bufio.NewScanner(f)
	for {
		for scanner.Scan() {
			line := scanner.Text()
			record, err := domain.ParseActivityLine(line)
			if err != nil {
				continue
			}
			if !domain.MatchesActivityFilter(record, filter) {
				continue
			}
			if jsonOutput {
				if err := outputEventNDJSON(record); err != nil {
					return err
				}
			} else {
				ts := record.Timestamp.UTC().Format(time.RFC3339)
				fmt.Printf("%-24s %-24s %-24s %s\n", ts, record.Scope.String(), record.Event, record.Detail)
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("follow: scan: %w", err)
		}

		// Sleep briefly then retry — the scanner will pick up new data.
		time.Sleep(500 * time.Millisecond)

		// Reset scanner error state for continued reading.
		scanner = bufio.NewScanner(f)
	}
}

// waitForFile polls until the file exists, returning an open handle.
func waitForFile(path string) (*os.File, error) {
	for {
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// outputEventNDJSON writes a single event as a JSON line (NDJSON format).
func outputEventNDJSON(r domain.EventRecord) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/atlascloudops/go-dscd/internal/cli"
)

var version = "0.1.0-dev"

func main() {
	root := cli.NewRootCommand(version)

	// Parse flags early so --log-level is available before any command runs.
	// Cobra parses persistent flags in PersistentPreRun, but slog must be
	// configured before that. We use ParseFlags here; Cobra tolerates being
	// parsed twice.
	_ = root.ParseFlags(os.Args[1:])

	logLevel := root.PersistentFlags().Lookup("log-level")
	level := parseSlogLevel(logLevel.Value.String())

	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(handler).With("version", version)
	slog.SetDefault(logger)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseSlogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

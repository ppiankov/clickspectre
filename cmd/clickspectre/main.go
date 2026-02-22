package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ppiankov/clickspectre/internal/app"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/spf13/cobra"
)

var (
	version    = "1.0.0-stage1"
	verbose    bool
	isFirstRun bool
)

// Exit codes for structured error reporting.
const (
	ExitSuccess    = 0
	ExitInternal   = 1
	ExitInvalidArg = 2
	ExitNotFound   = 3
	ExitNetwork    = 5
	ExitFindings   = 6
)

// FindingsError indicates the analysis completed but findings were detected.
type FindingsError struct {
	Count int
}

func (e *FindingsError) Error() string {
	return fmt.Sprintf("%d findings detected", e.Count)
}

func main() {
	logging.Init(false)
	isFirstRun = app.IsFirstRun()

	root := &cobra.Command{
		Use:   "clickspectre",
		Short: "ClickHouse usage analyzer",
		Long: `ClickSpectre analyzes ClickHouse query logs to determine
which tables are used, by whom, and which are safe to clean up.

It generates visual reports with interactive bipartite graphs showing
service-to-table relationships, usage statistics, and cleanup recommendations.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logging.Init(verbose)
		},
	}

	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "Verbose logging")
	root.SilenceUsage = true
	root.SilenceErrors = true

	root.AddCommand(NewAnalyzeCmd())
	root.AddCommand(NewServeCmd())
	root.AddCommand(NewDeployCmd())
	root.AddCommand(NewVersionCmd())

	if err := root.Execute(); err != nil {
		exitCode := classifyError(err)
		var fe *FindingsError
		if errors.As(err, &fe) {
			slog.Info("findings detected", slog.Int("count", fe.Count))
		} else {
			slog.Error("command failed", slog.String("error", err.Error()))
		}
		os.Exit(exitCode)
	}
}

func classifyError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	var fe *FindingsError
	if errors.As(err, &fe) {
		return ExitFindings
	}

	if os.IsNotExist(err) {
		return ExitNotFound
	}

	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "not a directory") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "no such file") {
		return ExitNotFound
	}

	if strings.Contains(msg, "dial") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable") {
		return ExitNetwork
	}

	if strings.Contains(msg, "required") ||
		strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "must be") ||
		strings.Contains(msg, "expected") {
		return ExitInvalidArg
	}

	return ExitInternal
}

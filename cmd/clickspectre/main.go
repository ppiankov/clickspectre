package main

import (
	"log/slog"
	"os"

	"github.com/ppiankov/clickspectre/internal/app"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/spf13/cobra"
)

var (
	version    = "1.0.0-stage1"
	verbose    bool
	isFirstRun bool
)

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

	root.AddCommand(NewAnalyzeCmd())
	root.AddCommand(NewServeCmd())
	root.AddCommand(NewDeployCmd())
	root.AddCommand(NewVersionCmd())

	if err := root.Execute(); err != nil {
		slog.Error("command failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

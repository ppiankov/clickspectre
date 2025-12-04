package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	version = "1.0.0-stage1"
)

func main() {
	root := &cobra.Command{
		Use:   "clickspectre",
		Short: "ClickHouse usage analyzer",
		Long: `ClickSpectre analyzes ClickHouse query logs to determine
which tables are used, by whom, and which are safe to clean up.

It generates visual reports with interactive bipartite graphs showing
service-to-table relationships, usage statistics, and cleanup recommendations.`,
	}

	root.AddCommand(NewAnalyzeCmd())
	root.AddCommand(NewServeCmd())
	root.AddCommand(NewDeployCmd())
	root.AddCommand(NewVersionCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

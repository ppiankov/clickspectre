package main

import (
	"encoding/json"
	"runtime"

	"github.com/spf13/cobra"
)

// NewVersionCmd creates the version command
func NewVersionCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			if format == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
					"version":  version,
					"commit":   commit,
					"go":       runtime.Version(),
					"platform": runtime.GOOS + "/" + runtime.GOARCH,
				})
			}
			cmd.Printf("%s (commit: %s)\n", version, commit)
			cmd.Printf("go: %s\n", runtime.Version())
			cmd.Printf("platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "", "Output format (json)")

	return cmd
}

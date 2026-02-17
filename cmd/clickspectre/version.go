package main

import (
	"runtime"

	"github.com/spf13/cobra"
)

// NewVersionCmd creates the version command
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(version)
			cmd.Printf("go: %s\n", runtime.Version())
			cmd.Printf("platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}

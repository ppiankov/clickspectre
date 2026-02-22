package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
)

// NewServeCmd creates the serve command
func NewServeCmd() *cobra.Command {
	var dir string
	var port int

	cmd := &cobra.Command{
		Use:   "serve [directory]",
		Short: "Serve static report directory",
		Long: `Start a local HTTP server to view the generated report.
The report will be available at http://localhost:PORT`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				dir = args[0]
			}

			return runServe(dir, port)
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./report", "Directory to serve")
	cmd.Flags().IntVar(&port, "port", 8080, "Port to serve on")

	return cmd
}

// runServe starts the HTTP server
func runServe(dir string, port int) error {
	// Validate directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("directory not found: %s", dir)
	}

	// Check report.json exists
	reportPath := filepath.Join(dir, "report.json")
	if _, err := os.Stat(reportPath); os.IsNotExist(err) {
		return fmt.Errorf("report.json not found in %s\nRun 'clickspectre analyze' first to generate a report", dir)
	}

	// Start server
	http.Handle("/", http.FileServer(http.Dir(dir)))
	addr := fmt.Sprintf(":%d", port)

	url := "http://localhost:" + strconv.Itoa(port)
	fmt.Fprintf(os.Stderr, "Serving %s at %s (Ctrl+C to stop)\n", dir, url)
	slog.Debug("report server started",
		slog.String("url", url),
		slog.String("dir", dir),
	)

	if err := http.ListenAndServe(addr, nil); err != nil {
		return fmt.Errorf("server stopped: %w", err)
	}
	return nil
}

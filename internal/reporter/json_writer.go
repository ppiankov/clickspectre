package reporter

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// WriteJSON writes the report to a JSON file
func WriteJSON(report *models.Report, cfg *config.Config) error {
	// Ensure output directory exists
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Marshal report to JSON with pretty printing
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report to JSON: %w", err)
	}

	// Write to file
	outputPath := filepath.Join(cfg.OutputDir, "report.json")
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write report.json: %w", err)
	}

	if cfg.Verbose {
		log.Printf("Report written to: %s", outputPath)
	}

	return nil
}

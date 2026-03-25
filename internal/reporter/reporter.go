package reporter

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// Reporter interface for generating reports
type Reporter interface {
	Generate(report *models.Report) error
	WriteAssets() error
}

// reporter implements the Reporter interface
type reporter struct {
	config *config.Config
}

// New creates a new reporter instance
func New(cfg *config.Config) Reporter {
	return &reporter{
		config: cfg,
	}
}

// IsStdout returns true when output should go to stdout.
func IsStdout(cfg *config.Config) bool {
	return cfg.OutputDir == "-"
}

// Generate generates the report
func (r *reporter) Generate(report *models.Report) error {
	if IsStdout(r.config) {
		return r.generateToStdout(report)
	}

	switch strings.ToLower(r.config.Format) {
	case "json":
		if err := WriteJSON(report, r.config); err != nil {
			return err
		}
		if err := r.WriteAssets(); err != nil {
			return err
		}
	case "text":
		if err := WriteText(report, r.config); err != nil {
			return err
		}
	case "sarif":
		if err := WriteSARIF(report, r.config); err != nil {
			return err
		}
	case "spectrehub":
		if err := WriteSpectreHub(report, r.config); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported format %q", r.config.Format)
	}

	return nil
}

func (r *reporter) generateToStdout(report *models.Report) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	switch strings.ToLower(r.config.Format) {
	case "json":
		return enc.Encode(report)
	case "text":
		return writeText(report, r.config, os.Stdout)
	case "sarif":
		sarif := buildSARIF(report, r.config)
		return enc.Encode(sarif)
	case "spectrehub":
		envelope := buildSpectreHub(report, r.config)
		return enc.Encode(envelope)
	default:
		return fmt.Errorf("unsupported format %q", r.config.Format)
	}
}

// WriteAssets writes static HTML/JS/CSS files to output directory
func (r *reporter) WriteAssets() error {
	return WriteAssets(r.config.OutputDir)
}

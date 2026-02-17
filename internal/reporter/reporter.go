package reporter

import (
	"fmt"
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

// Generate generates the report
func (r *reporter) Generate(report *models.Report) error {
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
	default:
		return fmt.Errorf("unsupported format %q", r.config.Format)
	}

	return nil
}

// WriteAssets writes static HTML/JS/CSS files to output directory
func (r *reporter) WriteAssets() error {
	return WriteAssets(r.config.OutputDir)
}

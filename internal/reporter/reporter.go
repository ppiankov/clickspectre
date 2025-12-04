package reporter

import (
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
	// Write JSON report
	if err := WriteJSON(report, r.config); err != nil {
		return err
	}

	// Write static assets
	if err := r.WriteAssets(); err != nil {
		return err
	}

	return nil
}

// WriteAssets writes static HTML/JS/CSS files to output directory
func (r *reporter) WriteAssets() error {
	return WriteAssets(r.config.OutputDir)
}

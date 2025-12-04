package analyzer

import (
	"context"
	"fmt"
	"log"

	"github.com/ppiankov/clickspectre/internal/k8s"
	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// Analyzer processes query log entries and builds analysis models
type Analyzer struct {
	config    *config.Config
	resolver  *k8s.Resolver
	tables    map[string]*models.Table
	services  map[string]*models.Service
	edges     []*models.Edge
	anomalies []*models.Anomaly
}

// New creates a new analyzer instance
func New(cfg *config.Config, resolver *k8s.Resolver) *Analyzer {
	return &Analyzer{
		config:    cfg,
		resolver:  resolver,
		tables:    make(map[string]*models.Table),
		services:  make(map[string]*models.Service),
		edges:     make([]*models.Edge, 0),
		anomalies: make([]*models.Anomaly, 0),
	}
}

// Analyze processes query log entries and builds all data models
func (a *Analyzer) Analyze(ctx context.Context, entries []*models.QueryLogEntry) error {
	if a.config.Verbose {
		log.Printf("Starting analysis of %d query log entries", len(entries))
	}

	// 1. Build table usage model
	if err := a.buildTableModel(entries); err != nil {
		return fmt.Errorf("failed to build table model: %w", err)
	}

	// 2. Build service model (with K8s resolution if enabled)
	if err := a.buildServiceModel(ctx, entries); err != nil {
		return fmt.Errorf("failed to build service model: %w", err)
	}

	// 3. Create service→table edges
	if err := a.buildEdges(entries); err != nil {
		return fmt.Errorf("failed to build edges: %w", err)
	}

	// 4. Detect anomalies (if enabled)
	if a.config.AnomalyDetection {
		if err := a.detectAnomalies(); err != nil {
			return fmt.Errorf("failed to detect anomalies: %w", err)
		}
	}

	// 5. Generate time series for sparklines
	if err := a.generateSparklines(entries); err != nil {
		return fmt.Errorf("failed to generate sparklines: %w", err)
	}

	if a.config.Verbose {
		log.Printf("Analysis complete: %d tables, %d services, %d edges, %d anomalies",
			len(a.tables), len(a.services), len(a.edges), len(a.anomalies))
	}

	return nil
}

// Tables returns the analyzed tables
func (a *Analyzer) Tables() map[string]*models.Table {
	return a.tables
}

// Services returns the analyzed services
func (a *Analyzer) Services() map[string]*models.Service {
	return a.services
}

// Edges returns the service→table edges
func (a *Analyzer) Edges() []*models.Edge {
	return a.edges
}

// Anomalies returns detected anomalies
func (a *Analyzer) Anomalies() []*models.Anomaly {
	return a.anomalies
}

package analyzer

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ppiankov/clickspectre/internal/k8s"
	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// CollectorInterface defines the methods needed from the collector
type CollectorInterface interface {
	FetchTableMetadata(ctx context.Context) (map[string]*models.Table, error)
}

// Analyzer processes query log entries and builds analysis models
type Analyzer struct {
	config    *config.Config
	resolver  *k8s.Resolver
	collector CollectorInterface
	tables    map[string]*models.Table
	services  map[string]*models.Service
	edges     []*models.Edge
	anomalies []*models.Anomaly
}

// New creates a new analyzer instance
func New(cfg *config.Config, resolver *k8s.Resolver, collector CollectorInterface) *Analyzer {
	return &Analyzer{
		config:    cfg,
		resolver:  resolver,
		collector: collector,
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

	// 1.5. Enrich with complete table inventory (if enabled)
	if a.config.DetectUnusedTables {
		if err := a.enrichWithCompleteInventory(ctx); err != nil {
			return fmt.Errorf("failed to enrich with table inventory: %w", err)
		}
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

// enrichWithCompleteInventory enriches the analysis with complete table inventory from system.tables
// This detects tables that have zero usage in the query logs
func (a *Analyzer) enrichWithCompleteInventory(ctx context.Context) error {
	if a.config.Verbose {
		log.Printf("Fetching complete table inventory from system.tables")
	}

	// 1. Fetch complete table inventory from system.tables
	allTables, err := a.collector.FetchTableMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch table metadata: %w", err)
	}

	if a.config.Verbose {
		log.Printf("Found %d tables in ClickHouse", len(allTables))
	}

	// 2. Merge with existing usage data
	zeroUsageCount := 0
	for fullName, metaTable := range allTables {
		if existing, found := a.tables[fullName]; found {
			// Table HAS usage - enrich with metadata
			existing.Engine = metaTable.Engine
			existing.IsReplicated = metaTable.IsReplicated
			existing.TotalBytes = metaTable.TotalBytes
			existing.TotalRows = metaTable.TotalRows
			existing.CreateTime = metaTable.CreateTime
			existing.IsMV = metaTable.IsMV
			existing.MVDependency = metaTable.MVDependency
			existing.ZeroUsage = false
		} else {
			// Table has ZERO usage - add as new entry
			metaTable.ZeroUsage = true
			metaTable.Reads = 0
			metaTable.Writes = 0
			metaTable.LastAccess = time.Time{}  // Zero value
			metaTable.FirstSeen = time.Time{}   // Zero value
			metaTable.Score = 0.0
			a.tables[fullName] = metaTable
			zeroUsageCount++
		}
	}

	if a.config.Verbose {
		log.Printf("Enriched with %d total tables, %d with zero usage", len(a.tables), zeroUsageCount)
	}

	return nil
}

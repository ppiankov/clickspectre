package collector

import (
	"context"
	"fmt"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// Collector interface for collecting ClickHouse query logs
type Collector interface {
	Collect(ctx context.Context) ([]*models.QueryLogEntry, error)
	Close() error
}

// collector implements the Collector interface
type collector struct {
	config *config.Config
	client *ClickHouseClient
	pool   *WorkerPool
}

// New creates a new collector instance
func New(cfg *config.Config) (Collector, error) {
	client, err := NewClickHouseClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create ClickHouse client: %w", err)
	}

	pool := NewWorkerPool(cfg.Concurrency)

	return &collector{
		config: cfg,
		client: client,
		pool:   pool,
	}, nil
}

// Collect retrieves and processes query log entries
func (c *collector) Collect(ctx context.Context) ([]*models.QueryLogEntry, error) {
	// Start the worker pool
	c.pool.Start(ctx)

	// Fetch query logs with pagination
	entries, err := c.client.FetchQueryLogs(ctx, c.config, c.pool)
	if err != nil {
		c.pool.Stop()
		return nil, fmt.Errorf("failed to fetch query logs: %w", err)
	}

	// Wait for all workers to finish processing
	c.pool.Stop()

	return entries, nil
}

// Close closes the collector and its resources
func (c *collector) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

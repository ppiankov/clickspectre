package collector

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// Collector interface for collecting ClickHouse query logs
type Collector interface {
	Collect(ctx context.Context) ([]*models.QueryLogEntry, error)
	FetchTableMetadata(ctx context.Context) (map[string]*models.Table, error)
	Close() error
	// CollectionMeta returns metadata about the last collection run.
	CollectionMeta() *models.CollectionMeta
	// QueryRaw executes a raw SQL query against the first available node.
	QueryRaw(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
}

// collector implements the Collector interface
type collector struct {
	config  *config.Config
	clients []*ClickHouseClient
	dsns    []string
	pool    *WorkerPool
	meta    *models.CollectionMeta
}

// New creates a new collector instance. Supports multiple DSNs for multi-node clusters.
func New(cfg *config.Config) (Collector, error) {
	dsns := cfg.ClickHouseDSNs
	if len(dsns) == 0 {
		dsns = []string{cfg.ClickHouseDSN}
	}

	var clients []*ClickHouseClient
	for _, dsn := range dsns {
		nodeCfg := *cfg
		nodeCfg.ClickHouseDSN = dsn
		client, err := NewClickHouseClient(&nodeCfg)
		if err != nil {
			// Close already-created clients
			for _, c := range clients {
				_ = c.Close()
			}
			return nil, fmt.Errorf("failed to create ClickHouse client for %s: %w", extractHost(dsn), err)
		}
		clients = append(clients, client)
	}

	pool := NewWorkerPool(cfg.Concurrency)

	return &collector{
		config:  cfg,
		clients: clients,
		dsns:    dsns,
		pool:    pool,
	}, nil
}

// Collect retrieves and processes query log entries from all nodes.
// When multiple nodes are configured, entries are collected concurrently
// and deduplicated by query_id.
func (c *collector) Collect(ctx context.Context) ([]*models.QueryLogEntry, error) {
	c.pool.Start(ctx)
	defer c.pool.Stop()

	if len(c.clients) == 1 {
		entries, err := c.clients[0].FetchQueryLogs(ctx, c.config, c.pool)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch query logs: %w", err)
		}
		c.meta = &models.CollectionMeta{
			Nodes:        []string{extractHost(c.dsns[0])},
			FailedNodes:  []string{},
			TotalEntries: len(entries),
		}
		return entries, nil
	}

	return c.collectMultiNode(ctx)
}

func (c *collector) collectMultiNode(ctx context.Context) ([]*models.QueryLogEntry, error) {
	type nodeResult struct {
		host    string
		entries []*models.QueryLogEntry
		err     error
	}

	results := make([]nodeResult, len(c.clients))
	var wg sync.WaitGroup

	for i, client := range c.clients {
		wg.Add(1)
		go func(idx int, cl *ClickHouseClient, dsn string) {
			defer wg.Done()
			host := extractHost(dsn)
			entries, err := cl.FetchQueryLogs(ctx, c.config, c.pool)
			results[idx] = nodeResult{host: host, entries: entries, err: err}
		}(i, client, c.dsns[i])
	}
	wg.Wait()

	// Merge results, track failures
	meta := &models.CollectionMeta{
		FailedNodes: []string{},
	}
	var allEntries []*models.QueryLogEntry
	var successCount int

	for _, r := range results {
		meta.Nodes = append(meta.Nodes, r.host)
		if r.err != nil {
			slog.Warn("node collection failed, continuing with remaining nodes",
				slog.String("node", r.host),
				slog.String("error", r.err.Error()))
			meta.FailedNodes = append(meta.FailedNodes, r.host)
			continue
		}
		allEntries = append(allEntries, r.entries...)
		successCount++
	}

	if successCount == 0 {
		return nil, fmt.Errorf("all nodes failed to collect query logs")
	}

	// Deduplicate by query_id
	beforeDedup := len(allEntries)
	allEntries = deduplicateByQueryID(allEntries)
	meta.TotalEntries = len(allEntries)
	meta.Deduplicated = beforeDedup - len(allEntries)
	c.meta = meta

	slog.Info("multi-node collection complete",
		slog.Int("nodes", len(c.clients)),
		slog.Int("failed", len(meta.FailedNodes)),
		slog.Int("entries", meta.TotalEntries),
		slog.Int("deduplicated", meta.Deduplicated))

	return allEntries, nil
}

// deduplicateByQueryID removes duplicate entries across nodes.
// Keeps the first occurrence (arbitrary — same query_id implies same data).
func deduplicateByQueryID(entries []*models.QueryLogEntry) []*models.QueryLogEntry {
	seen := make(map[string]bool, len(entries))
	result := make([]*models.QueryLogEntry, 0, len(entries))
	for _, e := range entries {
		if e.QueryID != "" && seen[e.QueryID] {
			continue
		}
		if e.QueryID != "" {
			seen[e.QueryID] = true
		}
		result = append(result, e)
	}
	return result
}

// FetchTableMetadata retrieves table metadata from system.tables.
// Uses the first available node.
func (c *collector) FetchTableMetadata(ctx context.Context) (map[string]*models.Table, error) {
	return c.clients[0].FetchTableMetadata(ctx)
}

// CollectionMeta returns metadata about the last collection run.
func (c *collector) CollectionMeta() *models.CollectionMeta {
	return c.meta
}

// QueryRaw executes a raw SQL query against the first available node.
func (c *collector) QueryRaw(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	if len(c.clients) == 0 {
		return nil, fmt.Errorf("no ClickHouse clients available")
	}
	return c.clients[0].conn.QueryContext(ctx, query, args...)
}

// Close closes the collector and all its client connections
func (c *collector) Close() error {
	var firstErr error
	for _, client := range c.clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// extractHost extracts the host from a DSN string for logging.
func extractHost(dsn string) string {
	// Reuse the existing extractHost from analyze.go — this is a collector-level version
	// that avoids importing cmd package.
	if len(dsn) < 10 {
		return "unknown"
	}
	for i := len("clickhouse://"); i < len(dsn); i++ {
		if dsn[i] == '@' {
			rest := dsn[i+1:]
			for j := range rest {
				if rest[j] == '/' || rest[j] == '?' {
					return rest[:j]
				}
			}
			return rest
		}
	}
	return "unknown"
}

package collector

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// ClickHouseClient handles ClickHouse connections and queries
type ClickHouseClient struct {
	conn   *sql.DB
	config *config.Config
}

// NewClickHouseClient creates a new ClickHouse client
func NewClickHouseClient(cfg *config.Config) (*ClickHouseClient, error) {
	// Parse DSN options
	opts, err := clickhouse.ParseDSN(cfg.ClickHouseDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ClickHouse DSN: %w", err)
	}

	// Set connection pooling
	opts.MaxOpenConns = 10
	opts.MaxIdleConns = 5
	opts.ConnMaxLifetime = time.Hour

	// Increase read timeout to prevent i/o timeouts
	opts.ReadTimeout = 10 * time.Minute
	opts.DialTimeout = 30 * time.Second

	// Don't set any query settings for potentially readonly users
	// The driver may try to set max_execution_time which fails in readonly mode
	opts.Settings = nil

	// Create connection
	conn := clickhouse.OpenDB(opts)
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping ClickHouse: %w", err)
	}

	if cfg.Verbose {
		slog.Debug("connected to ClickHouse", slog.String("addr", opts.Addr[0]))
	}

	return &ClickHouseClient{
		conn:   conn,
		config: cfg,
	}, nil
}

// CheckSchema verifies the query_log schema
func (c *ClickHouseClient) CheckSchema(ctx context.Context) error {
	query := "DESCRIBE TABLE system.query_log"

	rows, err := c.conn.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to describe query_log: %w", err)
	}
	defer func() { _ = rows.Close() }()

	slog.Debug("ClickHouse system.query_log schema", slog.String("table", "system.query_log"))
	for rows.Next() {
		var name, typ, defaultType, defaultExpr, comment, codecExpr, ttlExpr string
		if err := rows.Scan(&name, &typ, &defaultType, &defaultExpr, &comment, &codecExpr, &ttlExpr); err != nil {
			slog.Debug("failed to scan schema row", slog.String("error", err.Error()))
			continue
		}
		slog.Debug("schema column", slog.String("name", name), slog.String("type", typ))
	}

	return nil
}

// FetchQueryLogs retrieves query logs with pagination
func (c *ClickHouseClient) FetchQueryLogs(ctx context.Context, cfg *config.Config, pool *WorkerPool) ([]*models.QueryLogEntry, error) {
	lookbackDays := int(cfg.LookbackPeriod.Hours() / 24)
	queryCtx, cancel := withTotalTimeoutContext(ctx, cfg.QueryTimeout)
	defer cancel()

	// Check schema if verbose mode
	if cfg.Verbose {
		if err := c.CheckSchema(queryCtx); err != nil {
			slog.Debug("failed to check schema", slog.String("error", err.Error()))
		}
	}

	query := `
		SELECT
			query_id,
			type,
			event_time,
			query_kind,
			query,
			user,
			toString(initial_address) as client_ip,
			read_rows,
			written_rows,
			query_duration_ms,
			exception
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? DAY
		  AND type = 'QueryFinish'
		  AND query NOT LIKE '%system.query_log%'
		ORDER BY event_time DESC
		LIMIT ? OFFSET ?
	`

	var allEntries []*models.QueryLogEntry
	offset := 0
	totalProcessed := 0

	for {
		var rows *sql.Rows
		err := executeWithRetry(queryCtx, defaultRetryConfig(), func() error {
			var queryErr error
			rows, queryErr = c.conn.QueryContext(queryCtx, query, lookbackDays, cfg.BatchSize, offset)
			return queryErr
		})
		if err != nil {
			return nil, fmt.Errorf("query failed at offset %d: %w", offset, err)
		}

		batch, err := c.processBatch(rows)
		_ = rows.Close()

		if err != nil {
			return nil, fmt.Errorf("failed to process batch at offset %d: %w", offset, err)
		}

		if len(batch) == 0 {
			break // No more results
		}

		allEntries = append(allEntries, batch...)
		totalProcessed += len(batch)

		if cfg.Verbose {
			slog.Debug("processed query log entries",
				slog.Int("batch_count", len(batch)),
				slog.Int("total_processed", totalProcessed),
			)
		}

		// Check max rows limit
		if totalProcessed >= cfg.MaxRows {
			if cfg.Verbose {
				slog.Debug("max rows limit reached", slog.Int("max_rows", cfg.MaxRows))
			}
			break
		}

		// Check if we got less than batch size (last page)
		if len(batch) < cfg.BatchSize {
			break
		}

		offset += cfg.BatchSize
	}

	if cfg.Verbose {
		slog.Debug("total query log entries collected", slog.Int("total_entries", len(allEntries)))
	}

	return allEntries, nil
}

// processBatch processes a batch of rows from the query result
func (c *ClickHouseClient) processBatch(rows *sql.Rows) ([]*models.QueryLogEntry, error) {
	var entries []*models.QueryLogEntry
	rowNum := 0
	skippedRows := 0

	for rows.Next() {
		rowNum++
		var entry models.QueryLogEntry
		var durationMs uint64

		err := rows.Scan(
			&entry.QueryID,
			&entry.Type,
			&entry.EventTime,
			&entry.QueryKind,
			&entry.Query,
			&entry.User,
			&entry.ClientIP,
			&entry.ReadRows,
			&entry.WrittenRows,
			&durationMs,
			&entry.Exception,
		)
		if err != nil {
			skippedRows++
			// Always log the first error to help diagnose the issue
			if skippedRows == 1 {
				slog.Error("failed to scan first row",
					slog.String("error", err.Error()),
					slog.String("hint", "column type mismatch; run with --verbose for details"),
				)
			}
			if c.config.Verbose {
				slog.Debug("failed to scan row",
					slog.Int("row", rowNum),
					slog.String("error", err.Error()),
				)
			}
			// Try to skip this row and continue
			continue
		}

		// Validate essential fields
		if entry.QueryID == "" || entry.Query == "" {
			skippedRows++
			if c.config.Verbose {
				slog.Debug("row has empty essential fields", slog.Int("row", rowNum))
			}
			continue
		}

		// Truncate extremely long queries (handle in Go instead of SQL)
		if len(entry.Query) > 100000 {
			if c.config.Verbose {
				slog.Debug("row has very long query, truncating",
					slog.Int("row", rowNum),
					slog.Int("query_chars", len(entry.Query)),
				)
			}
			entry.Query = entry.Query[:100000] + "... [truncated]"
		}

		entry.Duration = time.Duration(durationMs) * time.Millisecond

		// Extract table references from query (with error recovery)
		func() {
			defer func() {
				if r := recover(); r != nil {
					if c.config.Verbose {
						slog.Debug("panic while extracting tables",
							slog.Int("row", rowNum),
							slog.String("panic", fmt.Sprint(r)),
						)
					}
					entry.Tables = []string{}
				}
			}()
			entry.Tables = extractTables(entry.Query)
		}()
		entry.Tables = c.filterExcludedTables(entry.Tables)

		entries = append(entries, &entry)
	}

	if skippedRows > 0 {
		slog.Error("skipped problematic rows",
			slog.Int("skipped_rows", skippedRows),
			slog.Int("total_rows", rowNum),
		)
	}

	// Check for iteration errors
	if err := rows.Err(); err != nil {
		// If we got some entries, return them with a warning rather than failing completely
		if len(entries) > 0 {
			slog.Error("error during row iteration",
				slog.Int("recovered_entries", len(entries)),
				slog.String("error", err.Error()),
			)
			return entries, nil
		}
		return nil, err
	}

	return entries, nil
}

// extractTables extracts table references from SQL query text
func extractTables(query string) []string {
	// Normalize query: convert to lowercase and remove extra spaces
	normalized := strings.ToLower(strings.TrimSpace(query))

	tables := make(map[string]bool) // Use map to deduplicate

	// Pattern 1: FROM clause - FROM [db.]table
	fromPattern := regexp.MustCompile(`from\s+([a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*|[a-z_][a-z0-9_]*)`)
	matches := fromPattern.FindAllStringSubmatch(normalized, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tables[match[1]] = true
		}
	}

	// Pattern 2: JOIN clause - JOIN [db.]table
	joinPattern := regexp.MustCompile(`join\s+([a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*|[a-z_][a-z0-9_]*)`)
	matches = joinPattern.FindAllStringSubmatch(normalized, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tables[match[1]] = true
		}
	}

	// Pattern 3: INSERT INTO - INSERT INTO [db.]table
	insertPattern := regexp.MustCompile(`insert\s+into\s+([a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*|[a-z_][a-z0-9_]*)`)
	matches = insertPattern.FindAllStringSubmatch(normalized, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tables[match[1]] = true
		}
	}

	// Pattern 4: CREATE TABLE - CREATE TABLE [IF NOT EXISTS] [db.]table
	createPattern := regexp.MustCompile(`create\s+(?:or\s+replace\s+)?table\s+(?:if\s+not\s+exists\s+)?([a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*|[a-z_][a-z0-9_]*)`)
	matches = createPattern.FindAllStringSubmatch(normalized, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tables[match[1]] = true
		}
	}

	// Convert map to slice
	var result []string
	for table := range tables {
		result = append(result, table)
	}

	return result
}

// FetchTableMetadata retrieves table metadata for MV detection
func (c *ClickHouseClient) FetchTableMetadata(ctx context.Context) (map[string]*models.Table, error) {
	query := `
		SELECT
			database,
			name,
			engine,
			total_bytes,
			total_rows,
			metadata_modification_time as create_time,
			arrayStringConcat(dependencies_database, ',') as dep_databases,
			arrayStringConcat(dependencies_table, ',') as dep_tables
		FROM system.tables
		WHERE database NOT IN ('system', 'information_schema', 'INFORMATION_SCHEMA')
		ORDER BY database, name
	`

	// Don't use timeout for readonly users
	rows, err := c.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch table metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tables := make(map[string]*models.Table)

	for rows.Next() {
		var database, name, engine string
		var totalBytes, totalRows sql.NullInt64
		var createTime time.Time
		var depDatabases, depTables sql.NullString

		if err := rows.Scan(&database, &name, &engine, &totalBytes, &totalRows, &createTime, &depDatabases, &depTables); err != nil {
			if c.config.Verbose {
				slog.Debug("failed to scan table metadata", slog.String("error", err.Error()))
			}
			continue
		}

		fullName := database + "." + name
		if c.config.IsTableExcluded(fullName) {
			continue
		}

		// Convert NULL-safe integers to uint64
		var bytesValue, rowsValue uint64
		if totalBytes.Valid {
			bytesValue = uint64(totalBytes.Int64)
		}
		if totalRows.Valid {
			rowsValue = uint64(totalRows.Int64)
		}

		table := &models.Table{
			Name:         name,
			Database:     database,
			FullName:     fullName,
			Engine:       engine,
			IsReplicated: strings.Contains(engine, "Replicated"),
			TotalBytes:   bytesValue,
			TotalRows:    rowsValue,
			CreateTime:   createTime,
			IsMV:         strings.HasPrefix(engine, "Materialized"),
			MVDependency: []string{},
			Sparkline:    []models.TimeSeriesPoint{}, // Initialize empty slice
		}

		// Parse comma-separated dependencies
		if depDatabases.Valid && depTables.Valid && depDatabases.String != "" && depTables.String != "" {
			databases := strings.Split(depDatabases.String, ",")
			tables := strings.Split(depTables.String, ",")
			for i := 0; i < len(databases) && i < len(tables); i++ {
				if databases[i] != "" && tables[i] != "" {
					dep := databases[i] + "." + tables[i]
					table.MVDependency = append(table.MVDependency, dep)
				}
			}
		}

		tables[fullName] = table
	}

	return tables, rows.Err()
}

func (c *ClickHouseClient) filterExcludedTables(tableNames []string) []string {
	if len(tableNames) == 0 {
		return []string{}
	}

	filtered := make([]string, 0, len(tableNames))
	for _, tableName := range tableNames {
		if tableName == "" || c.config.IsTableExcluded(tableName) {
			continue
		}
		filtered = append(filtered, tableName)
	}

	return filtered
}

// Close closes the ClickHouse connection
func (c *ClickHouseClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

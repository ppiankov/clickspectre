package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/ppiankov/clickspectre/pkg/config"
	"github.com/spf13/cobra"
)

// QueryResult represents a single row in query results.
type QueryResult struct {
	Key        string `json:"key"`
	QueryCount int64  `json:"query_count"`
	ReadRows   int64  `json:"read_rows"`
	LastSeen   string `json:"last_seen"`
}

// QueryOutput is the structured output of the query command.
type QueryOutput struct {
	Filter   string        `json:"filter"`
	GroupBy  string        `json:"group_by"`
	Lookback string        `json:"lookback"`
	Results  []QueryResult `json:"results"`
}

// NewQueryCmd creates the query command — ad-hoc query_log tool.
func NewQueryCmd() *cobra.Command {
	var (
		dsn         string
		table       string
		user        string
		ip          string
		groupBy     string
		lookback    string
		showQueries bool
		top         int
		minReadRows int64
		format      string
	)

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query ClickHouse query_log — the grep of ClickHouse",
		Long:  "Fast, targeted queries against system.query_log. Answers ad-hoc questions about table usage, user activity, and query patterns without running a full analysis.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var logOpts []logging.Option
			if quiet {
				logOpts = append(logOpts, logging.WithQuiet())
			}
			logging.Init(verbose, logOpts...)

			if dsn == "" {
				return fmt.Errorf("required flag --clickhouse-dsn not set")
			}

			dur, err := config.ParseDuration(lookback)
			if err != nil {
				return fmt.Errorf("invalid --lookback: %w", err)
			}

			cfg := config.DefaultConfig()
			cfg.ClickHouseDSN = dsn
			cfg.ClickHouseDSNs = strings.Split(dsn, ",")
			for i := range cfg.ClickHouseDSNs {
				cfg.ClickHouseDSNs[i] = strings.TrimSpace(cfg.ClickHouseDSNs[i])
			}

			col, err := collector.New(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect: %w", err)
			}
			defer func() { _ = col.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			results, err := runQuery(ctx, col, queryParams{
				table:       table,
				user:        user,
				ip:          ip,
				groupBy:     groupBy,
				lookback:    dur,
				showQueries: showQueries,
				top:         top,
				minReadRows: minReadRows,
			})
			if err != nil {
				return err
			}

			output := QueryOutput{
				Filter:   buildFilterDesc(table, user, ip),
				GroupBy:  groupBy,
				Lookback: lookback,
				Results:  results,
			}

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(output)
			}

			// Text output
			if len(results) == 0 {
				cmd.Println("No results found.")
				return nil
			}

			cmd.Printf("%-40s %10s %12s %s\n", strings.ToUpper(groupBy), "QUERIES", "READ_ROWS", "LAST_SEEN")
			cmd.Println(strings.Repeat("-", 80))
			for _, r := range results {
				cmd.Printf("%-40s %10d %12d %s\n", r.Key, r.QueryCount, r.ReadRows, r.LastSeen)
			}
			cmd.Printf("\n%d result(s)\n", len(results))

			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN (comma-separated for multi-node)")
	cmd.Flags().StringVar(&table, "table", "", "Filter by table name (supports wildcards)")
	cmd.Flags().StringVar(&user, "user", "", "Filter by user")
	cmd.Flags().StringVar(&ip, "ip", "", "Filter by client IP")
	cmd.Flags().StringVar(&groupBy, "by", "table", "Group results by: user, table, ip")
	cmd.Flags().StringVar(&lookback, "lookback", "24h", "Time window (e.g., 1h, 7d, 30d)")
	cmd.Flags().BoolVar(&showQueries, "show-queries", false, "Show sample queries")
	cmd.Flags().IntVar(&top, "top", 20, "Limit number of results")
	cmd.Flags().Int64Var(&minReadRows, "min-read-rows", 0, "Minimum read rows to include")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")

	return cmd
}

type queryParams struct {
	table       string
	user        string
	ip          string
	groupBy     string
	lookback    time.Duration
	showQueries bool
	top         int
	minReadRows int64
}

func runQuery(ctx context.Context, col collector.Collector, p queryParams) ([]QueryResult, error) {
	// Build the SQL dynamically based on groupBy and filters
	var groupCol string
	switch p.groupBy {
	case "user":
		groupCol = "user"
	case "ip":
		groupCol = "toString(initial_address)"
	case "table":
		groupCol = "arrayJoin(tables)"
	default:
		return nil, fmt.Errorf("invalid --by value %q: supported: user, table, ip", p.groupBy)
	}

	lookbackSeconds := int(p.lookback.Seconds())

	var conditions []string
	var args []interface{}

	conditions = append(conditions, "event_time >= now() - INTERVAL ? SECOND")
	args = append(args, lookbackSeconds)

	conditions = append(conditions, "type = 'QueryFinish'")
	conditions = append(conditions, "query NOT LIKE '%system.query_log%'")

	if p.table != "" {
		conditions = append(conditions, "has(tables, ?)")
		args = append(args, p.table)
	}
	if p.user != "" {
		conditions = append(conditions, "user = ?")
		args = append(args, p.user)
	}
	if p.ip != "" {
		conditions = append(conditions, "toString(initial_address) = ?")
		args = append(args, p.ip)
	}
	if p.minReadRows > 0 {
		conditions = append(conditions, "read_rows >= ?")
		args = append(args, p.minReadRows)
	}

	query := fmt.Sprintf(`
		SELECT
			%s AS group_key,
			count() AS query_count,
			sum(read_rows) AS total_read_rows,
			max(event_time) AS last_seen
		FROM system.query_log
		WHERE %s
		GROUP BY group_key
		ORDER BY query_count DESC
		LIMIT ?
	`, groupCol, strings.Join(conditions, " AND "))

	args = append(args, p.top)

	slog.Debug("executing query",
		slog.String("group_by", p.groupBy),
		slog.String("lookback", p.lookback.String()),
	)

	// Use the collector's underlying connection
	// We need access to the raw DB — use the QueryRaw method
	rows, err := col.QueryRaw(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []QueryResult
	for rows.Next() {
		var r QueryResult
		var readRows int64
		var lastSeen time.Time
		if err := rows.Scan(&r.Key, &r.QueryCount, &readRows, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		r.ReadRows = readRows
		r.LastSeen = lastSeen.Format(time.RFC3339)
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration failed: %w", err)
	}

	return results, nil
}

func buildFilterDesc(table, user, ip string) string {
	var parts []string
	if table != "" {
		parts = append(parts, "table="+table)
	}
	if user != "" {
		parts = append(parts, "user="+user)
	}
	if ip != "" {
		parts = append(parts, "ip="+ip)
	}
	if len(parts) == 0 {
		return "all"
	}
	return strings.Join(parts, ", ")
}

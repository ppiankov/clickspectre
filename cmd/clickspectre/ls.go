package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/ppiankov/clickspectre/pkg/config"
	"github.com/spf13/cobra"
)

// LSDatabase represents a database listing.
type LSDatabase struct {
	Name       string `json:"name"`
	TableCount int64  `json:"table_count"`
}

// LSTable represents a table listing.
type LSTable struct {
	Name    string  `json:"name"`
	Engine  string  `json:"engine"`
	SizeMB  float64 `json:"size_mb"`
	Rows    int64   `json:"rows"`
	Created string  `json:"created"`
}

// LSOutput is the structured output.
type LSOutput struct {
	Databases []LSDatabase `json:"databases,omitempty"`
	Tables    []LSTable    `json:"tables,omitempty"`
}

// NewLSCmd creates the ls command — tree/find for ClickHouse.
func NewLSCmd() *cobra.Command {
	var (
		dsn    string
		format string
		sortBy string
	)

	cmd := &cobra.Command{
		Use:   "ls [database]",
		Short: "List databases and tables — find/tree for ClickHouse",
		Long:  "List all databases, or tables within a specific database. Fast discovery without writing SQL.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var logOpts []logging.Option
			if quiet {
				logOpts = append(logOpts, logging.WithQuiet())
			}
			logging.Init(verbose, logOpts...)

			if dsn == "" {
				return fmt.Errorf("required flag --clickhouse-dsn not set")
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

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			if len(args) == 0 {
				return listDatabases(cmd, col, ctx, format)
			}
			return listTables(cmd, col, ctx, args[0], format, sortBy)
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")
	cmd.Flags().StringVar(&sortBy, "sort", "name", "Sort tables by: name, size, rows")

	return cmd
}

func listDatabases(cmd *cobra.Command, col collector.Collector, ctx context.Context, format string) error {
	rows, err := col.QueryRaw(ctx, `
		SELECT database, count() AS table_count
		FROM system.tables
		WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
		GROUP BY database
		ORDER BY database
	`)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var output LSOutput
	for rows.Next() {
		var d LSDatabase
		if err := rows.Scan(&d.Name, &d.TableCount); err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		output.Databases = append(output.Databases, d)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	if len(output.Databases) == 0 {
		cmd.Println("No databases found.")
		return nil
	}

	cmd.Printf("%-30s %s\n", "DATABASE", "TABLES")
	cmd.Println(strings.Repeat("-", 40))
	for _, d := range output.Databases {
		cmd.Printf("%-30s %d\n", d.Name, d.TableCount)
	}
	cmd.Printf("\n%d database(s)\n", len(output.Databases))
	return nil
}

func listTables(cmd *cobra.Command, col collector.Collector, ctx context.Context, database, format, sortBy string) error {
	var orderBy string
	switch sortBy {
	case "name":
		orderBy = "name"
	case "size":
		orderBy = "total_bytes DESC"
	case "rows":
		orderBy = "total_rows DESC"
	default:
		return fmt.Errorf("invalid --sort value %q: supported: name, size, rows", sortBy)
	}

	query := fmt.Sprintf(`
		SELECT name, engine,
			total_bytes / 1048576.0 AS size_mb,
			total_rows,
			metadata_modification_time
		FROM system.tables
		WHERE database = ?
		ORDER BY %s
	`, orderBy)

	rows, err := col.QueryRaw(ctx, query, database)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var output LSOutput
	for rows.Next() {
		var t LSTable
		var created time.Time
		if err := rows.Scan(&t.Name, &t.Engine, &t.SizeMB, &t.Rows, &created); err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		t.Created = created.Format("2006-01-02")
		output.Tables = append(output.Tables, t)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows error: %w", err)
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	if len(output.Tables) == 0 {
		cmd.Printf("No tables in database %q.\n", database)
		return nil
	}

	cmd.Printf("Tables in %s:\n\n", database)
	cmd.Printf("%-35s %-25s %10s %12s %s\n", "TABLE", "ENGINE", "SIZE_MB", "ROWS", "CREATED")
	cmd.Println(strings.Repeat("-", 95))
	for _, t := range output.Tables {
		cmd.Printf("%-35s %-25s %10.1f %12d %s\n", t.Name, t.Engine, t.SizeMB, t.Rows, t.Created)
	}
	cmd.Printf("\n%d table(s)\n", len(output.Tables))
	return nil
}

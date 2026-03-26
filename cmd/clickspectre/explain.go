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

// ExplainOutput is the structured table intelligence summary.
type ExplainOutput struct {
	Table       string            `json:"table"`
	Database    string            `json:"database"`
	Engine      string            `json:"engine"`
	SizeMB      float64           `json:"size_mb"`
	Rows        int64             `json:"rows"`
	Created     string            `json:"created"`
	Lookback    string            `json:"lookback"`
	TotalReads  int64             `json:"total_reads"`
	TotalWrites int64             `json:"total_writes"`
	TopUsers    []ExplainAccessor `json:"top_users"`
	TopIPs      []ExplainAccessor `json:"top_ips"`
	QueryKinds  []ExplainKind     `json:"query_kinds"`
	Status      string            `json:"status"` // active, low-usage, unused
}

// ExplainAccessor is a user or IP accessing the table.
type ExplainAccessor struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// ExplainKind is a query kind with count.
type ExplainKind struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
}

// NewExplainCmd creates the explain command.
func NewExplainCmd() *cobra.Command {
	var (
		dsn      string
		lookback string
		format   string
	)

	cmd := &cobra.Command{
		Use:   "explain <table>",
		Short: "Structured table intelligence — what is this table?",
		Long:  "Produce a structured summary of a table: metadata, usage, top users, top IPs, query patterns, and recommendation. Primary use case: agent context gathering.",
		Args:  cobra.ExactArgs(1),
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

			output, err := runExplain(col, args[0], dur, lookback)
			if err != nil {
				return err
			}

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(output)
			}

			printExplain(cmd, output)
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN")
	cmd.Flags().StringVar(&lookback, "lookback", "30d", "Analysis window")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")

	return cmd
}

func runExplain(col collector.Collector, table string, lookback time.Duration, lookbackStr string) (*ExplainOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Split table into database.name
	parts := strings.SplitN(table, ".", 2)
	var db, name string
	if len(parts) == 2 {
		db = parts[0]
		name = parts[1]
	} else {
		name = table
	}

	output := &ExplainOutput{
		Table:    name,
		Database: db,
		Lookback: lookbackStr,
	}

	// 1. Table metadata
	if db != "" {
		metaRows, err := col.QueryRaw(ctx, `
			SELECT engine, total_bytes / 1048576.0, total_rows, metadata_modification_time
			FROM system.tables WHERE database = ? AND name = ?
		`, db, name)
		if err == nil {
			if metaRows.Next() {
				var created time.Time
				_ = metaRows.Scan(&output.Engine, &output.SizeMB, &output.Rows, &created)
				output.Created = created.Format("2006-01-02")
			}
			_ = metaRows.Close()
		}
	}

	lookbackSec := int(lookback.Seconds())

	// 2. Read/write counts
	rwRows, err := col.QueryRaw(ctx, `
		SELECT
			countIf(query_kind = 'Select') AS reads,
			countIf(query_kind = 'Insert') AS writes
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish'
		  AND has(tables, ?)
	`, lookbackSec, table)
	if err == nil {
		if rwRows.Next() {
			_ = rwRows.Scan(&output.TotalReads, &output.TotalWrites)
		}
		_ = rwRows.Close()
	}

	// 3. Top users
	userRows, err := col.QueryRaw(ctx, `
		SELECT user, count() AS cnt
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish' AND has(tables, ?)
		GROUP BY user ORDER BY cnt DESC LIMIT 5
	`, lookbackSec, table)
	if err == nil {
		for userRows.Next() {
			var a ExplainAccessor
			if userRows.Scan(&a.Key, &a.Count) == nil {
				output.TopUsers = append(output.TopUsers, a)
			}
		}
		_ = userRows.Close()
	}

	// 4. Top IPs
	ipRows, err := col.QueryRaw(ctx, `
		SELECT toString(initial_address), count() AS cnt
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish' AND has(tables, ?)
		GROUP BY toString(initial_address) ORDER BY cnt DESC LIMIT 5
	`, lookbackSec, table)
	if err == nil {
		for ipRows.Next() {
			var a ExplainAccessor
			if ipRows.Scan(&a.Key, &a.Count) == nil {
				output.TopIPs = append(output.TopIPs, a)
			}
		}
		_ = ipRows.Close()
	}

	// 5. Query kinds
	kindRows, err := col.QueryRaw(ctx, `
		SELECT query_kind, count() AS cnt
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish' AND has(tables, ?)
		GROUP BY query_kind ORDER BY cnt DESC
	`, lookbackSec, table)
	if err == nil {
		for kindRows.Next() {
			var k ExplainKind
			if kindRows.Scan(&k.Kind, &k.Count) == nil {
				output.QueryKinds = append(output.QueryKinds, k)
			}
		}
		_ = kindRows.Close()
	}

	// 6. Status classification
	total := output.TotalReads + output.TotalWrites
	switch {
	case total == 0:
		output.Status = "unused"
	case total < 10:
		output.Status = "low-usage"
	default:
		output.Status = "active"
	}

	return output, nil
}

func printExplain(cmd *cobra.Command, o *ExplainOutput) {
	fullName := o.Table
	if o.Database != "" {
		fullName = o.Database + "." + o.Table
	}

	cmd.Printf("Table: %s\n", fullName)
	cmd.Printf("Engine: %s  Size: %.1f MB  Rows: %d  Created: %s\n", o.Engine, o.SizeMB, o.Rows, o.Created)
	cmd.Printf("Status: %s (lookback: %s)\n\n", o.Status, o.Lookback)

	cmd.Printf("Usage: %d reads, %d writes\n\n", o.TotalReads, o.TotalWrites)

	if len(o.TopUsers) > 0 {
		cmd.Println("Top users:")
		for _, u := range o.TopUsers {
			cmd.Printf("  %-20s %d queries\n", u.Key, u.Count)
		}
		cmd.Println()
	}

	if len(o.TopIPs) > 0 {
		cmd.Println("Top IPs:")
		for _, ip := range o.TopIPs {
			cmd.Printf("  %-20s %d queries\n", ip.Key, ip.Count)
		}
		cmd.Println()
	}

	if len(o.QueryKinds) > 0 {
		cmd.Println("Query kinds:")
		for _, k := range o.QueryKinds {
			cmd.Printf("  %-12s %d\n", k.Kind, k.Count)
		}
	}
}

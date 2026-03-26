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

// WhoEntry represents a service/user that accesses a table.
type WhoEntry struct {
	Key      string `json:"key"` // IP, user, or K8s service name
	Reads    int64  `json:"reads"`
	Writes   int64  `json:"writes"`
	LastSeen string `json:"last_seen"`
}

// WhoOutput is the structured output.
type WhoOutput struct {
	Table    string     `json:"table"`
	Lookback string     `json:"lookback"`
	GroupBy  string     `json:"group_by"`
	Entries  []WhoEntry `json:"entries"`
}

// NewWhoCmd creates the who command — reverse dependency lookup.
func NewWhoCmd() *cobra.Command {
	var (
		dsn      string
		lookback string
		groupBy  string
		format   string
		top      int
		stdin    bool
	)

	cmd := &cobra.Command{
		Use:   "who <table>",
		Short: "Show which services/users access a table",
		Long:  "Reverse dependency lookup: given a table name, show all services, users, or IPs that query it. Answer 'who depends on this table?' before making changes. Use --stdin to pipe table names.",
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

			// Collect table names from args or stdin
			var tables []string
			if stdin {
				tables = readStdinLines()
			} else if len(args) > 0 {
				tables = []string{args[0]}
			} else {
				return fmt.Errorf("provide a table name or use --stdin")
			}

			for _, table := range tables {
				table = strings.TrimSpace(table)
				if table == "" {
					continue
				}
				output, err := runWho(col, table, dur, groupBy, top)
				if err != nil {
					return err
				}
				output.Lookback = lookback

				if format == "json" {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					if err := enc.Encode(output); err != nil {
						return err
					}
				} else {
					printWho(cmd, output)
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN")
	cmd.Flags().StringVar(&lookback, "lookback", "7d", "Time window (e.g., 1h, 7d, 30d)")
	cmd.Flags().StringVar(&groupBy, "by", "ip", "Group by: ip, user")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "Read table names from stdin")
	cmd.Flags().IntVar(&top, "top", 20, "Limit results")

	return cmd
}

func runWho(col collector.Collector, table string, lookback time.Duration, groupBy string, top int) (*WhoOutput, error) {
	var groupCol string
	switch groupBy {
	case "ip":
		groupCol = "toString(initial_address)"
	case "user":
		groupCol = "user"
	default:
		return nil, fmt.Errorf("invalid --by value %q: supported: ip, user", groupBy)
	}

	lookbackSeconds := int(lookback.Seconds())

	query := fmt.Sprintf(`
		SELECT
			%s AS group_key,
			countIf(query_kind = 'Select') AS reads,
			countIf(query_kind = 'Insert') AS writes,
			max(event_time) AS last_seen
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish'
		  AND has(tables, ?)
		GROUP BY group_key
		ORDER BY (reads + writes) DESC
		LIMIT ?
	`, groupCol)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := col.QueryRaw(ctx, query, lookbackSeconds, table, top)
	if err != nil {
		return nil, fmt.Errorf("who query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	output := &WhoOutput{
		Table:   table,
		GroupBy: groupBy,
	}

	for rows.Next() {
		var e WhoEntry
		var lastSeen time.Time
		if err := rows.Scan(&e.Key, &e.Reads, &e.Writes, &lastSeen); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		e.LastSeen = lastSeen.Format("2006-01-02")
		output.Entries = append(output.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return output, nil
}

func printWho(cmd *cobra.Command, output *WhoOutput) {
	if len(output.Entries) == 0 {
		cmd.Printf("No access to %s in the last %s.\n", output.Table, output.Lookback)
		return
	}

	cmd.Printf("Who accesses %s (last %s, by %s):\n\n", output.Table, output.Lookback, output.GroupBy)
	cmd.Printf("%-30s %8s %8s %s\n", strings.ToUpper(output.GroupBy), "READS", "WRITES", "LAST_SEEN")
	cmd.Println(strings.Repeat("-", 60))
	for _, e := range output.Entries {
		cmd.Printf("%-30s %8d %8d %s\n", e.Key, e.Reads, e.Writes, e.LastSeen)
	}
	cmd.Printf("\n%d accessor(s)\n", len(output.Entries))
}

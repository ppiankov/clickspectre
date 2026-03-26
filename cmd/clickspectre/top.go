package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/ppiankov/clickspectre/pkg/config"
	"github.com/spf13/cobra"
)

// TopEntry represents a running query from system.processes.
type TopEntry struct {
	QueryID     string  `json:"query_id"`
	User        string  `json:"user"`
	ClientIP    string  `json:"client_ip"`
	Elapsed     float64 `json:"elapsed_seconds"`
	ReadRows    int64   `json:"read_rows"`
	MemoryUsage int64   `json:"memory_bytes"`
	Query       string  `json:"query"`
}

// TopOutput is the structured output of the top command.
type TopOutput struct {
	Timestamp string     `json:"timestamp"`
	Count     int        `json:"count"`
	Entries   []TopEntry `json:"entries"`
}

// NewTopCmd creates the top command — htop for ClickHouse.
func NewTopCmd() *cobra.Command {
	var (
		dsn        string
		watch      bool
		interval   string
		minElapsed float64
		user       string
		format     string
		top        int
	)

	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show running ClickHouse queries — htop for ClickHouse",
		Long:  "Query system.processes to show active queries with elapsed time, memory usage, and user. Use --watch for live refresh.",
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

			params := topParams{
				minElapsed: minElapsed,
				user:       user,
				top:        top,
			}

			if !watch {
				return runTopOnce(cmd, col, params, format)
			}

			dur, err := time.ParseDuration(interval)
			if err != nil {
				return fmt.Errorf("invalid --interval: %w", err)
			}
			return runTopWatch(cmd, col, params, format, dur)
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN (comma-separated for multi-node)")
	cmd.Flags().BoolVar(&watch, "watch", false, "Continuously refresh")
	cmd.Flags().StringVar(&interval, "interval", "2s", "Refresh interval for --watch")
	cmd.Flags().Float64Var(&minElapsed, "min-elapsed", 0, "Only show queries running longer than N seconds")
	cmd.Flags().StringVar(&user, "user", "", "Filter by user")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")
	cmd.Flags().IntVar(&top, "top", 20, "Limit number of results")

	return cmd
}

type topParams struct {
	minElapsed float64
	user       string
	top        int
}

func runTopOnce(cmd *cobra.Command, col collector.Collector, p topParams, format string) error {
	output, err := fetchTop(col, p)
	if err != nil {
		return err
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	printTop(cmd, output)
	return nil
}

func runTopWatch(cmd *cobra.Command, col collector.Collector, p topParams, format string, interval time.Duration) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately
	if output, err := fetchTop(col, p); err == nil {
		if format == "json" {
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(output)
		} else {
			fmt.Print("\033[2J\033[H") // Clear screen
			printTop(cmd, output)
		}
	}

	for {
		select {
		case <-ticker.C:
			output, err := fetchTop(col, p)
			if err != nil {
				cmd.PrintErrf("error: %v\n", err)
				continue
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				_ = enc.Encode(output)
			} else {
				fmt.Print("\033[2J\033[H") // Clear screen
				printTop(cmd, output)
			}
		case <-sigCh:
			return nil
		}
	}
}

func fetchTop(col collector.Collector, p topParams) (*TopOutput, error) {
	var conditions []string
	var args []interface{}

	conditions = append(conditions, "is_cancelled = 0")

	if p.minElapsed > 0 {
		conditions = append(conditions, "elapsed >= ?")
		args = append(args, p.minElapsed)
	}
	if p.user != "" {
		conditions = append(conditions, "user = ?")
		args = append(args, p.user)
	}

	query := fmt.Sprintf(`
		SELECT
			query_id,
			user,
			toString(address) AS client_ip,
			elapsed,
			read_rows,
			memory_usage,
			substring(query, 1, 200) AS query_short
		FROM system.processes
		WHERE %s
		ORDER BY elapsed DESC
		LIMIT ?
	`, strings.Join(conditions, " AND "))

	args = append(args, p.top)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := col.QueryRaw(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query system.processes failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	output := &TopOutput{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	for rows.Next() {
		var e TopEntry
		if err := rows.Scan(&e.QueryID, &e.User, &e.ClientIP, &e.Elapsed, &e.ReadRows, &e.MemoryUsage, &e.Query); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		output.Entries = append(output.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	output.Count = len(output.Entries)
	return output, nil
}

func printTop(cmd *cobra.Command, output *TopOutput) {
	cmd.Printf("clickspectre top — %s (%d queries)\n\n", output.Timestamp, output.Count)

	if output.Count == 0 {
		cmd.Println("No active queries.")
		return
	}

	cmd.Printf("%-12s %-14s %-16s %8s %12s %10s  %s\n",
		"QUERY_ID", "USER", "CLIENT_IP", "ELAPSED", "READ_ROWS", "MEMORY", "QUERY")
	cmd.Println(strings.Repeat("-", 100))

	for _, e := range output.Entries {
		qid := e.QueryID
		if len(qid) > 12 {
			qid = qid[:12]
		}
		q := strings.ReplaceAll(e.Query, "\n", " ")
		if len(q) > 40 {
			q = q[:40] + "..."
		}

		cmd.Printf("%-12s %-14s %-16s %7.1fs %12d %9s  %s\n",
			qid, e.User, e.ClientIP, e.Elapsed, e.ReadRows, formatBytes(e.MemoryUsage), q)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

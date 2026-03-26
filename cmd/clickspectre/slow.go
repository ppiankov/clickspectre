package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/ppiankov/clickspectre/pkg/config"
	"github.com/spf13/cobra"
)

// SlowPattern represents a group of similar queries.
type SlowPattern struct {
	Pattern     string  `json:"pattern"`
	Count       int64   `json:"count"`
	TotalMs     float64 `json:"total_duration_ms"`
	P50Ms       float64 `json:"p50_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
	AvgReadRows int64   `json:"avg_read_rows"`
	Example     string  `json:"example,omitempty"`
}

// SlowOutput is the structured output of the slow command.
type SlowOutput struct {
	Lookback string        `json:"lookback"`
	Patterns []SlowPattern `json:"patterns"`
}

// NewSlowCmd creates the slow command — pt-query-digest for ClickHouse.
func NewSlowCmd() *cobra.Command {
	var (
		dsn         string
		lookback    string
		minDuration string
		sortBy      string
		showExample bool
		format      string
		top         int
	)

	cmd := &cobra.Command{
		Use:   "slow",
		Short: "Slow query digest — pt-query-digest for ClickHouse",
		Long:  "Normalize queries by pattern, group, and show duration percentiles. Identifies which query patterns consume the most time.",
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

			var minDurMs float64
			if minDuration != "" {
				md, err := time.ParseDuration(minDuration)
				if err != nil {
					return fmt.Errorf("invalid --min-duration: %w", err)
				}
				minDurMs = float64(md.Milliseconds())
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

			output, err := runSlow(col, slowParams{
				lookback:    dur,
				minDurMs:    minDurMs,
				sortBy:      sortBy,
				showExample: showExample,
				top:         top,
			})
			if err != nil {
				return err
			}
			output.Lookback = lookback

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(output)
			}

			printSlow(cmd, output)
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN (comma-separated for multi-node)")
	cmd.Flags().StringVar(&lookback, "lookback", "24h", "Time window (e.g., 1h, 7d, 30d)")
	cmd.Flags().StringVar(&minDuration, "min-duration", "", "Only patterns with queries slower than this (e.g., 1s, 500ms)")
	cmd.Flags().StringVar(&sortBy, "sort", "duration", "Sort by: duration, count, read_rows")
	cmd.Flags().BoolVar(&showExample, "show-example", false, "Show one example query per pattern")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")
	cmd.Flags().IntVar(&top, "top", 20, "Limit number of patterns")

	return cmd
}

type slowParams struct {
	lookback    time.Duration
	minDurMs    float64
	sortBy      string
	showExample bool
	top         int
}

func runSlow(col collector.Collector, p slowParams) (*SlowOutput, error) {
	lookbackSeconds := int(p.lookback.Seconds())

	var orderBy string
	switch p.sortBy {
	case "duration":
		orderBy = "total_ms DESC"
	case "count":
		orderBy = "cnt DESC"
	case "read_rows":
		orderBy = "avg_rr DESC"
	default:
		return nil, fmt.Errorf("invalid --sort value %q: supported: duration, count, read_rows", p.sortBy)
	}

	var havingClause string
	var args []interface{}
	args = append(args, lookbackSeconds)

	if p.minDurMs > 0 {
		havingClause = "HAVING quantile(0.5)(query_duration_ms) >= ?"
		args = append(args, p.minDurMs)
	}

	// ClickHouse has normalizeQuery() for query normalization
	query := fmt.Sprintf(`
		SELECT
			normalizedQueryHash(query) AS qhash,
			count() AS cnt,
			sum(query_duration_ms) AS total_ms,
			quantile(0.5)(query_duration_ms) AS p50,
			quantile(0.95)(query_duration_ms) AS p95,
			quantile(0.99)(query_duration_ms) AS p99,
			avg(read_rows) AS avg_rr,
			any(substring(query, 1, 500)) AS example
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish'
		  AND query NOT LIKE '%%system.query_log%%'
		  AND query NOT LIKE '%%system.processes%%'
		GROUP BY qhash
		%s
		ORDER BY %s
		LIMIT ?
	`, havingClause, orderBy)

	args = append(args, p.top)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := col.QueryRaw(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("slow query analysis failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	output := &SlowOutput{}

	for rows.Next() {
		var s SlowPattern
		var qhash uint64
		if err := rows.Scan(&qhash, &s.Count, &s.TotalMs, &s.P50Ms, &s.P95Ms, &s.P99Ms, &s.AvgReadRows, &s.Example); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}
		s.Pattern = normalizeQuery(s.Example)
		if !p.showExample {
			s.Example = ""
		}
		output.Patterns = append(output.Patterns, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return output, nil
}

// Regex patterns for query normalization
var (
	reStringLiteral = regexp.MustCompile(`'[^']*'`)
	reNumericConst  = regexp.MustCompile(`\b\d+\.?\d*\b`)
	reInList        = regexp.MustCompile(`\bIN\s*\([^)]+\)`)
	reWhitespace    = regexp.MustCompile(`\s+`)
)

// normalizeQuery replaces literals with placeholders for pattern grouping.
func normalizeQuery(q string) string {
	q = reStringLiteral.ReplaceAllString(q, "'?'")
	q = reInList.ReplaceAllString(q, "IN (...)")
	q = reNumericConst.ReplaceAllString(q, "N")
	q = reWhitespace.ReplaceAllString(q, " ")
	q = strings.TrimSpace(q)
	if len(q) > 120 {
		q = q[:120] + "..."
	}
	return q
}

func printSlow(cmd *cobra.Command, output *SlowOutput) {
	if len(output.Patterns) == 0 {
		cmd.Println("No slow query patterns found.")
		return
	}

	cmd.Printf("Slow query digest (lookback: %s, %d patterns)\n\n", output.Lookback, len(output.Patterns))

	for i, p := range output.Patterns {
		cmd.Printf("#%d  count=%d  total=%.0fms  p50=%.0fms  p95=%.0fms  p99=%.0fms  avg_rows=%d\n",
			i+1, p.Count, p.TotalMs, p.P50Ms, p.P95Ms, p.P99Ms, p.AvgReadRows)
		cmd.Printf("    %s\n", p.Pattern)
		if p.Example != "" {
			example := strings.ReplaceAll(p.Example, "\n", " ")
			if len(example) > 120 {
				example = example[:120] + "..."
			}
			cmd.Printf("    example: %s\n", example)
		}
		cmd.Println()
	}
}

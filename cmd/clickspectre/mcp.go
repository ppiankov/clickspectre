package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/logging"
	mcpkg "github.com/ppiankov/clickspectre/internal/mcp"
	"github.com/ppiankov/clickspectre/pkg/config"
	"github.com/spf13/cobra"
)

// NewMCPCmd creates the mcp command — MCP server mode.
func NewMCPCmd() *cobra.Command {
	var dsn string

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP server for agent integration",
		Long:  "Start a Model Context Protocol server on stdio. Agents call clickspectre tools via JSON-RPC with a persistent ClickHouse connection pool.",
		RunE: func(cmd *cobra.Command, args []string) error {
			logging.Init(false, logging.WithQuiet())

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

			srv := mcpkg.NewServer("clickspectre", version)
			registerMCPTools(srv, col, cfg)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			return srv.Run(ctx)
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN (comma-separated for multi-node)")

	return cmd
}

func registerMCPTools(srv *mcpkg.Server, col collector.Collector, cfg *config.Config) {
	srv.RegisterTool(
		"clickspectre_query",
		"Search ClickHouse query_log by table, user, or IP",
		json.RawMessage(`{"type":"object","properties":{"table":{"type":"string","description":"Filter by table name"},"user":{"type":"string","description":"Filter by user"},"ip":{"type":"string","description":"Filter by client IP"},"by":{"type":"string","description":"Group by: user, table, ip","default":"table"},"lookback":{"type":"string","description":"Time window (e.g. 1h, 7d)","default":"24h"},"top":{"type":"integer","description":"Max results","default":20}}}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			p := queryParams{groupBy: "table", top: 20}
			if v, ok := args["table"].(string); ok {
				p.table = v
			}
			if v, ok := args["user"].(string); ok {
				p.user = v
			}
			if v, ok := args["ip"].(string); ok {
				p.ip = v
			}
			if v, ok := args["by"].(string); ok {
				p.groupBy = v
			}
			if v, ok := args["top"].(float64); ok {
				p.top = int(v)
			}
			lookback := "24h"
			if v, ok := args["lookback"].(string); ok {
				lookback = v
			}
			dur, err := config.ParseDuration(lookback)
			if err != nil {
				return "", err
			}
			p.lookback = dur
			results, err := runQuery(ctx, col, p)
			if err != nil {
				return "", err
			}
			data, _ := json.MarshalIndent(results, "", "  ")
			return string(data), nil
		},
	)

	srv.RegisterTool(
		"clickspectre_who",
		"Show which services/users access a ClickHouse table",
		json.RawMessage(`{"type":"object","properties":{"table":{"type":"string","description":"Table name (required)"},"by":{"type":"string","description":"Group by: ip, user","default":"ip"},"lookback":{"type":"string","description":"Time window","default":"7d"},"top":{"type":"integer","default":20}},"required":["table"]}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			table, _ := args["table"].(string)
			if table == "" {
				return "", fmt.Errorf("table is required")
			}
			groupBy := "ip"
			if v, ok := args["by"].(string); ok {
				groupBy = v
			}
			lookback := "7d"
			if v, ok := args["lookback"].(string); ok {
				lookback = v
			}
			top := 20
			if v, ok := args["top"].(float64); ok {
				top = int(v)
			}
			dur, _ := config.ParseDuration(lookback)
			output, err := runWho(col, table, dur, groupBy, top)
			if err != nil {
				return "", err
			}
			data, _ := json.MarshalIndent(output, "", "  ")
			return string(data), nil
		},
	)

	srv.RegisterTool(
		"clickspectre_ls",
		"List ClickHouse databases or tables in a database",
		json.RawMessage(`{"type":"object","properties":{"database":{"type":"string","description":"Database name (omit for database list)"}}}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			database, _ := args["database"].(string)
			queryCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()

			if database == "" {
				rows, err := col.QueryRaw(queryCtx, `SELECT database, count() FROM system.tables WHERE database NOT IN ('system','INFORMATION_SCHEMA','information_schema') GROUP BY database ORDER BY database`)
				if err != nil {
					return "", err
				}
				defer func() { _ = rows.Close() }()
				var result []map[string]interface{}
				for rows.Next() {
					var name string
					var count int64
					if rows.Scan(&name, &count) == nil {
						result = append(result, map[string]interface{}{"database": name, "table_count": count})
					}
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				return string(data), nil
			}

			rows, err := col.QueryRaw(queryCtx, `SELECT name, engine, total_bytes/1048576.0, total_rows FROM system.tables WHERE database = ? ORDER BY name`, database)
			if err != nil {
				return "", err
			}
			defer func() { _ = rows.Close() }()
			var result []map[string]interface{}
			for rows.Next() {
				var name, engine string
				var sizeMB float64
				var rowCount int64
				if rows.Scan(&name, &engine, &sizeMB, &rowCount) == nil {
					result = append(result, map[string]interface{}{"name": name, "engine": engine, "size_mb": sizeMB, "rows": rowCount})
				}
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return string(data), nil
		},
	)

	srv.RegisterTool(
		"clickspectre_explain",
		"Get structured intelligence about a ClickHouse table",
		json.RawMessage(`{"type":"object","properties":{"table":{"type":"string","description":"Table name (database.table format)"},"lookback":{"type":"string","default":"30d"}},"required":["table"]}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			table, _ := args["table"].(string)
			if table == "" {
				return "", fmt.Errorf("table is required")
			}
			lookback := "30d"
			if v, ok := args["lookback"].(string); ok {
				lookback = v
			}
			dur, _ := config.ParseDuration(lookback)
			output, err := runExplain(col, table, dur, lookback)
			if err != nil {
				return "", err
			}
			data, _ := json.MarshalIndent(output, "", "  ")
			return string(data), nil
		},
	)

	srv.RegisterTool(
		"clickspectre_top",
		"Show currently running ClickHouse queries",
		json.RawMessage(`{"type":"object","properties":{"min_elapsed":{"type":"number","description":"Min elapsed seconds","default":0},"user":{"type":"string"},"top":{"type":"integer","default":20}}}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			p := topParams{top: 20}
			if v, ok := args["min_elapsed"].(float64); ok {
				p.minElapsed = v
			}
			if v, ok := args["user"].(string); ok {
				p.user = v
			}
			if v, ok := args["top"].(float64); ok {
				p.top = int(v)
			}
			output, err := fetchTop(col, p)
			if err != nil {
				return "", err
			}
			data, _ := json.MarshalIndent(output, "", "  ")
			return string(data), nil
		},
	)

	srv.RegisterTool(
		"clickspectre_slow",
		"Analyze slow query patterns with duration percentiles",
		json.RawMessage(`{"type":"object","properties":{"lookback":{"type":"string","default":"24h"},"sort":{"type":"string","description":"duration, count, read_rows","default":"duration"},"top":{"type":"integer","default":20}}}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			lookback := "24h"
			if v, ok := args["lookback"].(string); ok {
				lookback = v
			}
			sortBy := "duration"
			if v, ok := args["sort"].(string); ok {
				sortBy = v
			}
			top := 20
			if v, ok := args["top"].(float64); ok {
				top = int(v)
			}
			dur, _ := config.ParseDuration(lookback)
			output, err := runSlow(col, slowParams{lookback: dur, sortBy: sortBy, top: top})
			if err != nil {
				return "", err
			}
			data, _ := json.MarshalIndent(output, "", "  ")
			return string(data), nil
		},
	)

	srv.RegisterTool(
		"clickspectre_grants",
		"List ClickHouse users and their permissions",
		json.RawMessage(`{"type":"object","properties":{"user":{"type":"string","description":"Filter to specific user"},"unused":{"type":"boolean","description":"Only show users with zero queries"},"lookback":{"type":"string","default":"30d"}}}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			filterUser, _ := args["user"].(string)
			unused, _ := args["unused"].(bool)
			lookback := "30d"
			if v, ok := args["lookback"].(string); ok {
				lookback = v
			}
			var dur time.Duration
			if unused {
				dur, _ = config.ParseDuration(lookback)
			}
			output, err := runGrants(col, filterUser, unused, dur)
			if err != nil {
				return "", err
			}
			data, _ := json.MarshalIndent(output, "", "  ")
			return string(data), nil
		},
	)

	srv.RegisterTool(
		"clickspectre_doctor",
		"Check ClickHouse connectivity and configuration",
		json.RawMessage(`{"type":"object","properties":{}}`),
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			output := runDoctor(cfg.ClickHouseDSN, "")
			data, _ := json.MarshalIndent(output, "", "  ")
			return string(data), nil
		},
	)
}

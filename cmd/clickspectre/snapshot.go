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

// Snapshot captures cluster state for offline analysis.
type Snapshot struct {
	Schema    string          `json:"schema"`
	Tool      string          `json:"tool"`
	Version   string          `json:"version"`
	Timestamp string          `json:"timestamp"`
	Cluster   SnapshotCluster `json:"cluster"`
	Databases []SnapshotDB    `json:"databases"`
	Users     []SnapshotUser  `json:"users"`
	Activity  []SnapshotUsage `json:"activity"`
}

// SnapshotCluster holds cluster-level info.
type SnapshotCluster struct {
	CHVersion   string `json:"clickhouse_version"`
	Uptime      int64  `json:"uptime_seconds"`
	TotalDBs    int    `json:"total_databases"`
	TotalTables int    `json:"total_tables"`
}

// SnapshotDB holds per-database info.
type SnapshotDB struct {
	Name   string          `json:"name"`
	Tables []SnapshotTable `json:"tables"`
}

// SnapshotTable holds per-table metadata.
type SnapshotTable struct {
	Name    string  `json:"name"`
	Engine  string  `json:"engine"`
	SizeMB  float64 `json:"size_mb"`
	Rows    int64   `json:"rows"`
	Created string  `json:"created"`
}

// SnapshotUser holds user info.
type SnapshotUser struct {
	Name     string `json:"name"`
	AuthType string `json:"auth_type"`
}

// SnapshotUsage holds aggregated per-table usage.
type SnapshotUsage struct {
	Table      string `json:"table"`
	QueryCount int64  `json:"query_count"`
	LastSeen   string `json:"last_seen"`
}

// NewSnapshotCmd creates the snapshot command.
func NewSnapshotCmd() *cobra.Command {
	var (
		dsn      string
		output   string
		lookback string
	)

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Save cluster state for offline analysis",
		Long:  "Capture databases, tables, users, and query activity to a JSON file. Use with --from-file on other commands for offline work.",
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

			snap, err := buildSnapshot(col, dur)
			if err != nil {
				return err
			}

			data, err := json.MarshalIndent(snap, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal snapshot: %w", err)
			}

			if output == "-" {
				_, err = os.Stdout.Write(data)
				return err
			}

			if err := os.WriteFile(output, data, 0644); err != nil {
				return fmt.Errorf("write snapshot: %w", err)
			}
			cmd.Printf("Snapshot saved to %s (%d bytes)\n", output, len(data))
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN")
	cmd.Flags().StringVarP(&output, "output", "o", "snapshot.json", "Output file (use - for stdout)")
	cmd.Flags().StringVar(&lookback, "lookback", "30d", "Activity lookback period")

	return cmd
}

func buildSnapshot(col collector.Collector, lookback time.Duration) (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	snap := &Snapshot{
		Schema:    "clickspectre/snapshot/v1",
		Tool:      "clickspectre",
		Version:   version,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// 1. Cluster info
	row, err := col.QueryRaw(ctx, "SELECT version(), uptime()")
	if err == nil {
		if row.Next() {
			_ = row.Scan(&snap.Cluster.CHVersion, &snap.Cluster.Uptime)
		}
		_ = row.Close()
	}

	// 2. Databases and tables
	dbRows, err := col.QueryRaw(ctx, `
		SELECT database, name, engine,
			total_bytes / 1048576.0 AS size_mb,
			total_rows,
			metadata_modification_time
		FROM system.tables
		WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
		ORDER BY database, name
	`)
	if err != nil {
		return nil, fmt.Errorf("query tables: %w", err)
	}

	dbMap := make(map[string]*SnapshotDB)
	for dbRows.Next() {
		var dbName string
		var t SnapshotTable
		var created time.Time
		if err := dbRows.Scan(&dbName, &t.Name, &t.Engine, &t.SizeMB, &t.Rows, &created); err != nil {
			_ = dbRows.Close()
			return nil, fmt.Errorf("scan table: %w", err)
		}
		t.Created = created.Format("2006-01-02")

		db, ok := dbMap[dbName]
		if !ok {
			db = &SnapshotDB{Name: dbName}
			dbMap[dbName] = db
		}
		db.Tables = append(db.Tables, t)
		snap.Cluster.TotalTables++
	}
	_ = dbRows.Close()

	for _, db := range dbMap {
		snap.Databases = append(snap.Databases, *db)
	}
	snap.Cluster.TotalDBs = len(dbMap)

	// 3. Users
	userRows, err := col.QueryRaw(ctx, "SELECT name, auth_type FROM system.users ORDER BY name")
	if err == nil {
		for userRows.Next() {
			var u SnapshotUser
			if userRows.Scan(&u.Name, &u.AuthType) == nil {
				snap.Users = append(snap.Users, u)
			}
		}
		_ = userRows.Close()
	}

	// 4. Aggregated activity per table
	seconds := int(lookback.Seconds())
	actRows, err := col.QueryRaw(ctx, `
		SELECT arrayJoin(tables) AS tbl, count() AS cnt, max(event_time) AS last_seen
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish'
		  AND query NOT LIKE '%system.query_log%'
		GROUP BY tbl
		ORDER BY cnt DESC
		LIMIT 500
	`, seconds)
	if err == nil {
		for actRows.Next() {
			var a SnapshotUsage
			var lastSeen time.Time
			if actRows.Scan(&a.Table, &a.QueryCount, &lastSeen) == nil {
				a.LastSeen = lastSeen.Format(time.RFC3339)
				snap.Activity = append(snap.Activity, a)
			}
		}
		_ = actRows.Close()
	}

	return snap, nil
}

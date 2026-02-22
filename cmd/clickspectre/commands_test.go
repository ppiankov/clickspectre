package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/analyzer"
	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func TestNewAnalyzeCmdPreRunValidation(t *testing.T) {
	tests := []struct {
		name         string
		lookback     string
		queryTimeout string
		cacheTTL     string
		format       string
		wantErr      string
	}{
		{
			name:         "valid_durations",
			lookback:     "7d",
			queryTimeout: "30m",
			cacheTTL:     "2m",
			format:       "json",
			wantErr:      "",
		},
		{
			name:         "valid_sarif_format",
			lookback:     "7d",
			queryTimeout: "30m",
			cacheTTL:     "2m",
			format:       "sarif",
			wantErr:      "",
		},
		{
			name:         "valid_text_format",
			lookback:     "7d",
			queryTimeout: "30m",
			cacheTTL:     "2m",
			format:       "text",
			wantErr:      "",
		},
		{
			name:         "invalid_lookback",
			lookback:     "bad",
			queryTimeout: "30m",
			cacheTTL:     "2m",
			format:       "json",
			wantErr:      "invalid --lookback duration",
		},
		{
			name:         "invalid_query_timeout",
			lookback:     "7d",
			queryTimeout: "bad",
			cacheTTL:     "2m",
			format:       "json",
			wantErr:      "invalid --query-timeout duration",
		},
		{
			name:         "invalid_cache_ttl",
			lookback:     "7d",
			queryTimeout: "30m",
			cacheTTL:     "bad",
			format:       "json",
			wantErr:      "invalid --k8s-cache-ttl duration",
		},
		{
			name:         "invalid_format",
			lookback:     "7d",
			queryTimeout: "30m",
			cacheTTL:     "2m",
			format:       "yaml",
			wantErr:      "invalid --format value",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewAnalyzeCmd()

			if err := cmd.Flags().Set("clickhouse-dsn", "clickhouse://localhost:9000/default"); err != nil {
				t.Fatalf("failed to set clickhouse-dsn flag: %v", err)
			}
			if err := cmd.Flags().Set("lookback", tc.lookback); err != nil {
				t.Fatalf("failed to set lookback flag: %v", err)
			}
			if err := cmd.Flags().Set("query-timeout", tc.queryTimeout); err != nil {
				t.Fatalf("failed to set query-timeout flag: %v", err)
			}
			if err := cmd.Flags().Set("k8s-cache-ttl", tc.cacheTTL); err != nil {
				t.Fatalf("failed to set k8s-cache-ttl flag: %v", err)
			}
			if err := cmd.Flags().Set("format", tc.format); err != nil {
				t.Fatalf("failed to set format flag: %v", err)
			}

			err := cmd.PreRunE(cmd, nil)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}

			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestNewAnalyzeCmdCompatibilityAliases(t *testing.T) {
	cmd := NewAnalyzeCmd()

	hasAuditAlias := false
	for _, alias := range cmd.Aliases {
		if alias == "audit" {
			hasAuditAlias = true
			break
		}
	}
	if !hasAuditAlias {
		t.Fatal("expected analyze command to include audit alias")
	}

	cmd = NewAnalyzeCmd()
	if err := cmd.Flags().Set("clickhouse-url", "clickhouse://localhost:9000/default"); err != nil {
		t.Fatalf("failed to set clickhouse-url alias flag: %v", err)
	}
	if err := cmd.Flags().Set("lookback", "7d"); err != nil {
		t.Fatalf("failed to set lookback flag: %v", err)
	}
	if err := cmd.Flags().Set("query-timeout", "30m"); err != nil {
		t.Fatalf("failed to set query-timeout flag: %v", err)
	}
	if err := cmd.Flags().Set("k8s-cache-ttl", "2m"); err != nil {
		t.Fatalf("failed to set k8s-cache-ttl flag: %v", err)
	}
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("expected clickhouse-url alias to satisfy required DSN, got %v", err)
	}
}

func TestNewAnalyzeCmdAutoLoadsConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	configContent := "clickhouse_url: clickhouse://localhost:9000/default\nformat: text\ntimeout: 2m\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".clickspectre.yaml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cmd := NewAnalyzeCmd()
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("expected auto-loaded config file to satisfy PreRun validation, got %v", err)
	}
}

func TestNewAnalyzeCmdConfigFlagLoadsCustomPath(t *testing.T) {
	tempDir := t.TempDir()
	customPath := filepath.Join(tempDir, "custom-config.yaml")
	configContent := "clickhouse_url: clickhouse://localhost:9000/default\n"
	if err := os.WriteFile(customPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write custom config file: %v", err)
	}

	cmd := NewAnalyzeCmd()
	if err := cmd.Flags().Set("config", customPath); err != nil {
		t.Fatalf("failed to set config flag: %v", err)
	}
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("expected --config path to load successfully, got %v", err)
	}
}

func TestNewAnalyzeCmdFlagsOverrideConfigFileValues(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	// Config file intentionally contains invalid format and timeout values.
	configContent := "clickhouse_url: clickhouse://from-config:9000/default\nformat: yaml\ntimeout: bad-duration\n"
	if err := os.WriteFile(filepath.Join(tempDir, ".clickspectre.yaml"), []byte(configContent), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cmd := NewAnalyzeCmd()
	if err := cmd.Flags().Set("clickhouse-dsn", "clickhouse://from-cli:9000/default"); err != nil {
		t.Fatalf("failed to set clickhouse-dsn flag: %v", err)
	}
	if err := cmd.Flags().Set("format", "json"); err != nil {
		t.Fatalf("failed to set format flag: %v", err)
	}
	if err := cmd.Flags().Set("query-timeout", "1m"); err != nil {
		t.Fatalf("failed to set query-timeout flag: %v", err)
	}

	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("expected CLI flags to override invalid config-file values, got %v", err)
	}
}

func TestRunAnalyzeFailsOnInvalidDSN(t *testing.T) {

	cfg := config.DefaultConfig()

	cfg.ClickHouseDSN = "://invalid"

	err := runAnalyze(cfg, false)

	if err == nil {

		t.Fatal("expected error, got nil")

	}

	if !strings.Contains(err.Error(), "failed to create collector") {

		t.Fatalf("expected collector creation error, got %v", err)

	}

}

func TestBuildReportIncludesAnalyzedData(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ClickHouseDSN = "clickhouse://localhost:9000/default"
	cfg.AnomalyDetection = false

	entries := []*models.QueryLogEntry{
		{
			QueryID:   "q1",
			EventTime: time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC),
			Query:     "SELECT * FROM db.events",
			ClientIP:  "10.0.0.1",
			Tables:    []string{"db.events"},
			ReadRows:  100,
		},
	}

	an := analyzer.New(cfg, nil, nil)
	if err := an.Analyze(context.Background(), entries); err != nil {
		t.Fatalf("analyze failed: %v", err)
	}

	report := buildReport(cfg, entries, an, models.CleanupRecommendations{
		SafeToDrop: []string{"db.old_events"},
	}, time.Now().Add(-2*time.Second))

	if report.Tool != "clickspectre" {
		t.Fatalf("expected tool to be %q, got %q", "clickspectre", report.Tool)
	}
	if report.Version != version {
		t.Fatalf("expected report version to be %q, got %q", version, report.Version)
	}
	parsedTimestamp, err := time.Parse(time.RFC3339, report.Timestamp)
	if err != nil {
		t.Fatalf("expected RFC3339 timestamp, got %q: %v", report.Timestamp, err)
	}
	if report.Timestamp != report.Metadata.GeneratedAt.UTC().Format(time.RFC3339) {
		t.Fatalf("expected timestamp to match metadata.generated_at at RFC3339 precision, got %q and %q", report.Timestamp, report.Metadata.GeneratedAt.UTC().Format(time.RFC3339))
	}
	if parsedTimestamp.Location() != time.UTC {
		t.Fatalf("expected UTC timestamp, got location %q", parsedTimestamp.Location())
	}

	if report.Metadata.TotalQueriesAnalyzed != 1 {
		t.Fatalf("expected total queries 1, got %d", report.Metadata.TotalQueriesAnalyzed)
	}
	if report.Metadata.ClickHouseHost == "unknown" {
		t.Fatalf("expected extracted host, got %q", report.Metadata.ClickHouseHost)
	}
	if len(report.Tables) == 0 {
		t.Fatal("expected at least one table in report")
	}
	if len(report.Services) == 0 {
		t.Fatal("expected at least one service in report")
	}
	if len(report.Edges) == 0 {
		t.Fatal("expected at least one edge in report")
	}
	if len(report.CleanupRecommendations.SafeToDrop) != 1 {
		t.Fatal("expected recommendations to be included")
	}
}

func TestHelpers(t *testing.T) {
	longDSN := "clickhouse://very-long-and-sensitive-dsn-value"
	masked := maskDSN(longDSN)
	if !strings.HasPrefix(masked, longDSN[:20]) || !strings.HasSuffix(masked, "...***") {
		t.Fatalf("unexpected masked DSN: %q", masked)
	}
	if got := maskDSN("short"); got != "***" {
		t.Fatalf("expected short dsn mask to be ***, got %q", got)
	}

	host := extractHost("clickhouse://localhost:9000/default")
	if host == "unknown" {
		t.Fatalf("expected extracted host, got %q", host)
	}
	if got := extractHost("short"); got != "unknown" {
		t.Fatalf("expected unknown host for short dsn, got %q", got)
	}

	if got := min(3, 7); got != 3 {
		t.Fatalf("expected min(3, 7) = 3, got %d", got)
	}
	if got := min(9, 2); got != 2 {
		t.Fatalf("expected min(9, 2) = 2, got %d", got)
	}
}

func TestBuildAnalysisSummaryNoFindings(t *testing.T) {
	report := &models.Report{
		Metadata: models.Metadata{
			ClickHouseHost:       "localhost",
			TotalQueriesAnalyzed: 17,
		},
		Tables: []models.Table{
			{Database: "db1", FullName: "db1.events"},
			{Database: "db2", FullName: "db2.metrics"},
		},
		Services: []models.Service{{IP: "10.0.0.1"}},
	}

	summary := buildAnalysisSummary(report)
	if summary.clickHouseHost != "localhost" {
		t.Fatalf("expected host localhost, got %q", summary.clickHouseHost)
	}
	if summary.databaseCount != 2 {
		t.Fatalf("expected 2 databases, got %d", summary.databaseCount)
	}
	if summary.tableCount != 2 {
		t.Fatalf("expected 2 tables, got %d", summary.tableCount)
	}
	if summary.serviceCount != 1 {
		t.Fatalf("expected 1 service, got %d", summary.serviceCount)
	}
	if summary.queryCount != 17 {
		t.Fatalf("expected 17 queries, got %d", summary.queryCount)
	}
	if summary.findingCount != 0 {
		t.Fatalf("expected 0 findings, got %d", summary.findingCount)
	}
}

func TestBuildAnalysisSummaryWithFindings(t *testing.T) {
	report := &models.Report{
		Metadata: models.Metadata{},
		Tables: []models.Table{
			{FullName: "analytics.events"},
			{FullName: "analytics.sessions"},
		},
		Services: []models.Service{
			{IP: "10.0.0.1"},
			{IP: "10.0.0.2"},
		},
		Anomalies: []models.Anomaly{
			{Type: "stale_table", Severity: "medium", AffectedTable: "analytics.sessions"},
		},
		CleanupRecommendations: models.CleanupRecommendations{
			SafeToDrop: []string{"analytics.old_events"},
			LikelySafe: []string{"analytics.old_sessions"},
		},
	}

	summary := buildAnalysisSummary(report)
	if summary.clickHouseHost != "unknown" {
		t.Fatalf("expected unknown host fallback, got %q", summary.clickHouseHost)
	}
	if summary.databaseCount != 1 {
		t.Fatalf("expected 1 database from full-name fallback, got %d", summary.databaseCount)
	}
	if summary.tableCount != 2 {
		t.Fatalf("expected 2 tables, got %d", summary.tableCount)
	}
	if summary.serviceCount != 2 {
		t.Fatalf("expected 2 services, got %d", summary.serviceCount)
	}
	if summary.findingCount != 3 {
		t.Fatalf("expected 3 findings, got %d", summary.findingCount)
	}
}

func TestServeCommandAndRunServeValidation(t *testing.T) {
	cmd := NewServeCmd()
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Fatal("expected args validation error for too many arguments")
	}

	if err := runServe(filepath.Join(t.TempDir(), "missing"), 8080); err == nil || !strings.Contains(err.Error(), "directory not found") {
		t.Fatalf("expected missing directory error, got %v", err)
	}

	dir := t.TempDir()
	if err := runServe(dir, 8080); err == nil || !strings.Contains(err.Error(), "report.json not found") {
		t.Fatalf("expected missing report.json error, got %v", err)
	}
}

func TestDeployCommandAndRunDeployValidation(t *testing.T) {
	cmd := NewDeployCmd()
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Fatal("expected args validation error for too many arguments")
	}

	if err := runDeploy("", "default", filepath.Join(t.TempDir(), "missing"), 8080, false, ""); err == nil || !strings.Contains(err.Error(), "report directory not found") {
		t.Fatalf("expected missing report dir error, got %v", err)
	}

	dir := t.TempDir()
	if err := runDeploy("", "default", dir, 8080, false, ""); err == nil || !strings.Contains(err.Error(), "report.json not found") {
		t.Fatalf("expected missing report.json error, got %v", err)
	}
}

func TestVersionCommandAndQuantityParser(t *testing.T) {
	if err := NewVersionCmd().Execute(); err != nil {
		t.Fatalf("version command execution failed: %v", err)
	}

	q := mustParseQuantity("64Mi")
	if q.String() != "64Mi" {
		t.Fatalf("expected quantity 64Mi, got %s", q.String())
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid quantity")
		}
	}()
	_ = mustParseQuantity("not-a-quantity")
}

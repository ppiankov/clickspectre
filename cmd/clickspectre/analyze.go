package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/analyzer"
	"github.com/ppiankov/clickspectre/internal/baseline"
	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/k8s"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/internal/reporter"
	"github.com/ppiankov/clickspectre/internal/scorer"
	"github.com/ppiankov/clickspectre/pkg/config"
	"github.com/spf13/cobra"
)

// NewAnalyzeCmd creates the analyze command
func NewAnalyzeCmd() *cobra.Command {
	cfg := config.DefaultConfig()

	// String variables for custom duration parsing
	var lookbackStr string
	var queryTimeoutStr string
	var k8sCacheTTLStr string
	var configPath string

	cmd := &cobra.Command{
		Use:     "analyze",
		Aliases: []string{"audit"},
		Short:   "Analyze ClickHouse usage and generate report",
		Long: `Analyze ClickHouse query logs to determine table usage patterns,
generate cleanup recommendations, and create an interactive visual report.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			loadedConfigPath, err := applyAnalyzeConfigFileDefaults(cmd, cfg, &queryTimeoutStr, configPath)
			if err != nil {
				return err
			}

			if loadedConfigPath != "" {
				slog.Debug("loaded config file", slog.String("path", loadedConfigPath))
			}

			// Parse custom durations
			if lookbackStr != "" {
				cfg.LookbackPeriod, err = config.ParseDuration(lookbackStr)
				if err != nil {
					return fmt.Errorf("invalid --lookback duration: %w", err)
				}
			}

			if queryTimeoutStr != "" {
				cfg.QueryTimeout, err = config.ParseDuration(queryTimeoutStr)
				if err != nil {
					return fmt.Errorf("invalid --query-timeout duration: %w", err)
				}
			}

			if k8sCacheTTLStr != "" {
				cfg.K8sCacheTTL, err = config.ParseDuration(k8sCacheTTLStr)
				if err != nil {
					return fmt.Errorf("invalid --k8s-cache-ttl duration: %w", err)
				}
			}

			if cfg.ClickHouseDSN == "" {
				return fmt.Errorf("required flag(s) \"clickhouse-dsn\" or \"clickhouse-url\" not set")
			}

			cfg.Format = strings.ToLower(cfg.Format)
			cfg.Normalize()
			switch cfg.Format {
			case "json", "text", "sarif":
			default:
				return fmt.Errorf("invalid --format value: %q (supported: json, text, sarif)", cfg.Format)
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.Verbose = verbose
			return runAnalyze(cfg, isFirstRun)
		}}

	// ClickHouse flags
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file (default: auto-load .clickspectre.yaml)")
	cmd.Flags().StringVar(&cfg.ClickHouseDSN, "clickhouse-dsn", "", "ClickHouse DSN")
	cmd.Flags().StringVar(&cfg.ClickHouseDSN, "clickhouse-url", "", "Deprecated alias for --clickhouse-dsn")
	_ = cmd.Flags().MarkDeprecated("clickhouse-url", "use --clickhouse-dsn instead")

	cmd.Flags().StringVar(&queryTimeoutStr, "query-timeout", "5m", "Query timeout (e.g., 5m, 10m, 1h)")
	cmd.Flags().IntVar(&cfg.BatchSize, "batch-size", 100000, "Query log batch size")
	cmd.Flags().IntVar(&cfg.MaxRows, "max-rows", 1000000, "Max query log rows to process")
	cmd.Flags().StringVar(&lookbackStr, "lookback", "30d", "Lookback period (e.g., 7d, 30d, 90d, 720h)")

	// Kubernetes flags
	cmd.Flags().BoolVar(&cfg.ResolveK8s, "resolve-k8s", false, "Enable Kubernetes IP resolution")
	cmd.Flags().StringVar(&cfg.KubeConfig, "kubeconfig", "", "Path to kubeconfig (default: ~/.kube/config)")
	cmd.Flags().StringVar(&k8sCacheTTLStr, "k8s-cache-ttl", "5m", "Kubernetes cache TTL (e.g., 5m, 10m, 1h)")
	cmd.Flags().IntVar(&cfg.K8sRateLimit, "k8s-rate-limit", 10, "Kubernetes API rate limit (requests/sec)")

	// Concurrency flags
	cmd.Flags().IntVar(&cfg.Concurrency, "concurrency", 5, "Worker pool size")

	// Output flags
	cmd.Flags().StringVar(&cfg.OutputDir, "output", "./report", "Output directory")
	cmd.Flags().StringVar(&cfg.Format, "format", "json", "Output format (json|text|sarif)")
	cmd.Flags().StringVar(&cfg.BaselinePath, "baseline", "", "Path to baseline file for suppressing known findings")
	cmd.Flags().BoolVar(&cfg.UpdateBaseline, "update-baseline", false, "Update baseline with current findings")

	// Analysis flags
	cmd.Flags().StringVar(&cfg.ScoringAlgorithm, "scoring-algorithm", "simple", "Scoring algorithm (simple)")
	cmd.Flags().BoolVar(&cfg.AnomalyDetection, "anomaly-detection", true, "Enable anomaly detection")
	cmd.Flags().BoolVar(&cfg.IncludeMVDeps, "include-mv-deps", true, "Include materialized view dependencies")
	cmd.Flags().BoolVar(&cfg.DetectUnusedTables, "detect-unused-tables", false, "Detect tables with zero usage in query logs")
	cmd.Flags().Float64Var(&cfg.MinTableSizeMB, "min-table-size", 1.0, "Minimum table size in MB for unused table recommendations")
	cmd.Flags().Uint64Var(&cfg.MinQueryCount, "min-query-count", 0, "Minimum query count required to consider a table active")
	cmd.Flags().StringSliceVar(&cfg.ExcludeTables, "exclude-table", []string{}, "Exclude table pattern (repeatable, supports glob)")
	cmd.Flags().StringSliceVar(&cfg.ExcludeDatabases, "exclude-database", []string{}, "Exclude database pattern (repeatable, supports glob)")

	// Operational flags
	cmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Dry run mode (don't write output)")

	return cmd
}

func applyAnalyzeConfigFileDefaults(
	cmd *cobra.Command,
	cfg *config.Config,
	queryTimeoutStr *string,
	configPath string,
) (string, error) {
	flags := cmd.Flags()

	var (
		fileCfg *config.FileConfig
		path    string
		err     error
	)

	if strings.TrimSpace(configPath) != "" {
		fileCfg, err = config.LoadFile(configPath)
		if err != nil {
			return "", err
		}
		path = configPath
	} else {
		fileCfg, path, err = config.AutoLoadFile()
		if err != nil {
			return "", err
		}
	}

	if fileCfg == nil {
		return "", nil
	}

	if !flags.Changed("clickhouse-dsn") && !flags.Changed("clickhouse-url") {
		if endpoint := fileCfg.ClickHouseEndpoint(); endpoint != "" {
			cfg.ClickHouseDSN = endpoint
		}
	}
	if !flags.Changed("format") && fileCfg.Format != "" {
		cfg.Format = fileCfg.Format
	}
	if !flags.Changed("query-timeout") {
		if timeout := fileCfg.QueryTimeoutValue(); timeout != "" {
			*queryTimeoutStr = timeout
		}
	}
	if !flags.Changed("exclude-table") && len(fileCfg.ExcludeTables) > 0 {
		cfg.ExcludeTables = append([]string(nil), fileCfg.ExcludeTables...)
	}
	if !flags.Changed("exclude-database") && len(fileCfg.ExcludeDatabases) > 0 {
		cfg.ExcludeDatabases = append([]string(nil), fileCfg.ExcludeDatabases...)
	}
	if !flags.Changed("min-query-count") && fileCfg.MinQueryCount != nil {
		cfg.MinQueryCount = *fileCfg.MinQueryCount
	}
	if !flags.Changed("min-table-size") && fileCfg.MinTableSizeMB != nil {
		cfg.MinTableSizeMB = *fileCfg.MinTableSizeMB
	}

	return path, nil
}

// runAnalyze executes the analysis workflow
func runAnalyze(cfg *config.Config, isFirstRun bool) error {
	logging.Init(cfg.Verbose)
	if isFirstRun {
		slog.Debug("first-time analysis", slog.String("clickhouse_host", extractHost(cfg.ClickHouseDSN)))
	}

	startTime := time.Now()
	ctx := context.Background()

	slog.Debug("starting analysis",
		slog.String("clickhouse_dsn", maskDSN(cfg.ClickHouseDSN)),
		slog.Duration("lookback", cfg.LookbackPeriod),
		slog.Int("concurrency", cfg.Concurrency),
		slog.Int("batch_size", cfg.BatchSize),
		slog.Int("max_rows", cfg.MaxRows),
		slog.String("k8s_resolution", strconv.FormatBool(cfg.ResolveK8s)),
	)
	logConnectionSettings(cfg)

	// 1. Initialize collector
	slog.Debug("connecting to ClickHouse", slog.String("dsn", maskDSN(cfg.ClickHouseDSN)))
	col, err := collector.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create collector: %w", err)
	}
	defer func() { _ = col.Close() }()

	// 2. Initialize K8s resolver (if enabled)
	var resolver *k8s.Resolver
	if cfg.ResolveK8s {
		slog.Debug("connecting to Kubernetes", slog.String("kubeconfig", cfg.KubeConfig))
		resolver, err = k8s.NewResolver(cfg)
		if err != nil {
			slog.Error("failed to initialize Kubernetes resolver",
				slog.String("error", err.Error()),
				slog.String("fallback", "continuing without Kubernetes resolution"),
			)
			cfg.ResolveK8s = false
		}
	}

	// 3. Collect query logs
	slog.Debug("collecting query logs",
		slog.Duration("lookback", cfg.LookbackPeriod),
		slog.Int("batch_size", cfg.BatchSize),
	)
	entries, err := col.Collect(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect query logs: %w", err)
	}
	slog.Debug("collected query log entries", slog.Int("count", len(entries)))

	// 4. Analyze data
	slog.Debug("analyzing data", slog.Int("entries", len(entries)))
	an := analyzer.New(cfg, resolver, col)
	if err := an.Analyze(ctx, entries); err != nil {
		return fmt.Errorf("failed to analyze data: %w", err)
	}
	slog.Debug("analysis complete",
		slog.Int("tables", len(an.Tables())),
		slog.Int("services", len(an.Services())),
		slog.Int("edges", len(an.Edges())),
	)

	// 5. Score tables and generate recommendations
	slog.Debug("scoring tables", slog.Int("tables", len(an.Tables())))
	recommendations := scorer.GenerateRecommendations(an.Tables(), an.Services(), cfg)
	slog.Debug("recommendations generated",
		slog.Int("safe_to_drop", len(recommendations.SafeToDrop)),
		slog.Int("likely_safe", len(recommendations.LikelySafe)),
		slog.Int("keep", len(recommendations.Keep)),
	)

	// 6. Build report
	report := buildReport(cfg, entries, an, recommendations, startTime)

	// 7. Apply baseline (if enabled)
	if err := applyBaseline(cfg, report); err != nil {
		return err
	}

	// 8. Write output
	if !cfg.DryRun {
		slog.Debug("writing report", slog.String("output_dir", cfg.OutputDir))
		rep := reporter.New(cfg)
		if err := rep.Generate(report); err != nil {
			return fmt.Errorf("failed to generate report: %w", err)
		}
		slog.Debug("report written", slog.String("output_dir", cfg.OutputDir))
	} else {
		slog.Debug("dry run enabled", slog.String("output_dir", cfg.OutputDir))
	}

	// 9. Success
	duration := time.Since(startTime)
	logAnalysisSummary(cfg, report, duration)

	if isFirstRun {
		slog.Debug("first run complete", slog.String("tip", "review the report in your browser"))
	}

	findingCount := countFindings(report)
	if findingCount > 0 {
		return &FindingsError{Count: findingCount}
	}

	return nil
}

// buildReport constructs the final report
func buildReport(
	cfg *config.Config,
	entries []*models.QueryLogEntry,
	an *analyzer.Analyzer,
	recommendations models.CleanupRecommendations,
	startTime time.Time,
) *models.Report {
	generatedAt := time.Now().UTC()

	// Convert maps to slices
	var tables []models.Table
	for _, table := range an.Tables() {
		tables = append(tables, *table)
	}

	var services []models.Service
	for _, service := range an.Services() {
		services = append(services, *service)
	}

	var edges []models.Edge
	for _, edge := range an.Edges() {
		edges = append(edges, *edge)
	}

	var anomalies []models.Anomaly
	for _, anomaly := range an.Anomalies() {
		anomalies = append(anomalies, *anomaly)
	}

	// Extract host from DSN
	host := extractHost(cfg.ClickHouseDSN)

	return &models.Report{
		Tool:      "clickspectre",
		Version:   version,
		Timestamp: generatedAt.Format(time.RFC3339),
		Metadata: models.Metadata{
			GeneratedAt:          generatedAt,
			LookbackDays:         int(cfg.LookbackPeriod.Hours() / 24),
			ClickHouseHost:       host,
			TotalQueriesAnalyzed: uint64(len(entries)),
			AnalysisDuration:     time.Since(startTime).Round(time.Second).String(),
			Version:              version,
			K8sResolutionEnabled: cfg.ResolveK8s,
		},
		Tables:                 tables,
		Services:               services,
		Edges:                  edges,
		Anomalies:              anomalies,
		CleanupRecommendations: recommendations,
	}
}

// maskDSN masks sensitive information in DSN
func maskDSN(dsn string) string {
	// Simple masking - just show protocol and host
	if len(dsn) > 20 {
		return dsn[:20] + "...***"
	}
	return "***"
}

// extractHost extracts host from DSN
func extractHost(dsn string) string {
	if dsn == "" {
		return "unknown"
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		return "unknown"
	}

	if host := parsed.Hostname(); host != "" {
		return host
	}

	return "unknown"
}

func logConnectionSettings(cfg *config.Config) {
	slog.Debug("connection settings",
		slog.String("clickhouse_host", extractHost(cfg.ClickHouseDSN)),
		slog.Duration("lookback", cfg.LookbackPeriod),
		slog.Duration("query_timeout", cfg.QueryTimeout),
	)
}

type analysisSummary struct {
	clickHouseHost string
	databaseCount  int
	tableCount     int
	serviceCount   int
	queryCount     uint64
	findingCount   int
}

func buildAnalysisSummary(report *models.Report) analysisSummary {
	host := report.Metadata.ClickHouseHost
	if host == "" {
		host = "unknown"
	}

	return analysisSummary{
		clickHouseHost: host,
		databaseCount:  countDatabases(report.Tables),
		tableCount:     len(report.Tables),
		serviceCount:   len(report.Services),
		queryCount:     report.Metadata.TotalQueriesAnalyzed,
		findingCount:   countFindings(report),
	}
}

func logAnalysisSummary(cfg *config.Config, report *models.Report, duration time.Duration) {
	summary := buildAnalysisSummary(report)
	message := "analysis completed with findings"
	if summary.findingCount == 0 {
		message = "analysis completed with no findings"
	}

	attrs := []any{
		slog.String("clickhouse_host", summary.clickHouseHost),
		slog.Int("database_count", summary.databaseCount),
		slog.Int("table_count", summary.tableCount),
		slog.Int("service_count", summary.serviceCount),
		slog.Uint64("query_count", summary.queryCount),
		slog.Int("finding_count", summary.findingCount),
		slog.Duration("duration", duration.Round(time.Second)),
		slog.Bool("dry_run", cfg.DryRun),
	}
	if !cfg.DryRun {
		attrs = append(attrs, slog.String("output_dir", cfg.OutputDir))
	}

	slog.Debug(message, attrs...)
}

func countDatabases(tables []models.Table) int {
	unique := make(map[string]struct{})
	for _, table := range tables {
		database := strings.TrimSpace(table.Database)
		if database == "" && strings.Contains(table.FullName, ".") {
			parts := strings.SplitN(table.FullName, ".", 2)
			database = strings.TrimSpace(parts[0])
		}
		if database == "" {
			continue
		}
		unique[database] = struct{}{}
	}
	return len(unique)
}

func countFindings(report *models.Report) int {
	recommendationFindings := len(report.CleanupRecommendations.ZeroUsageNonReplicated) +
		len(report.CleanupRecommendations.ZeroUsageReplicated) +
		len(report.CleanupRecommendations.SafeToDrop) +
		len(report.CleanupRecommendations.LikelySafe)

	return recommendationFindings + len(report.Anomalies)
}

func applyBaseline(cfg *config.Config, report *models.Report) error {
	// Determine baseline file path
	baselinePath := cfg.BaselinePath
	if baselinePath == "" {
		baselinePath = baseline.DefaultPath
	}

	// Load existing baseline findings
	existingBaselineFindings, err := baseline.Load(baselinePath)
	if err != nil {
		return fmt.Errorf("failed to load baseline from %s: %w", baselinePath, err)
	}

	// Generate current findings from the report
	currentFindings, err := baseline.GenerateFindings(report)
	if err != nil {
		return fmt.Errorf("failed to generate findings for baseline: %w", err)
	}

	// If --baseline flag is used (but not --update-baseline), apply suppression
	if cfg.BaselinePath != "" && !cfg.UpdateBaseline {
		suppressedCount, err := baseline.ApplySuppression(report, existingBaselineFindings)
		if err != nil {
			return fmt.Errorf("failed to apply baseline suppression: %w", err)
		}
		if suppressedCount > 0 {
			slog.Debug("suppressed known findings",
				slog.Int("suppressed", suppressedCount),
				slog.String("baseline_file", baselinePath),
			)
		}
	}

	// If --update-baseline flag is used, merge current findings into the baseline and save
	if cfg.UpdateBaseline {
		mergedFindings := baseline.MergeFindings(existingBaselineFindings, currentFindings)
		if err := baseline.Save(baselinePath, mergedFindings); err != nil {
			return fmt.Errorf("failed to save updated baseline to %s: %w", baselinePath, err)
		}
		slog.Debug("baseline updated",
			slog.String("baseline_file", baselinePath),
			slog.Int("total_findings", len(mergedFindings)),
		)
	}

	return nil
}

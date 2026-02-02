package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ppiankov/clickspectre/internal/analyzer"
	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/k8s"
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

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze ClickHouse usage and generate report",
		Long: `Analyze ClickHouse query logs to determine table usage patterns,
generate cleanup recommendations, and create an interactive visual report.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Parse custom durations
			var err error

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

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyze(cfg)
		},
	}

	// ClickHouse flags
	cmd.Flags().StringVar(&cfg.ClickHouseDSN, "clickhouse-dsn", "", "ClickHouse DSN (required)")
	_ = cmd.MarkFlagRequired("clickhouse-dsn") // Error only occurs if flag doesn't exist

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
	cmd.Flags().StringVar(&cfg.Format, "format", "json", "Output format (json)")

	// Analysis flags
	cmd.Flags().StringVar(&cfg.ScoringAlgorithm, "scoring-algorithm", "simple", "Scoring algorithm (simple)")
	cmd.Flags().BoolVar(&cfg.AnomalyDetection, "anomaly-detection", true, "Enable anomaly detection")
	cmd.Flags().BoolVar(&cfg.IncludeMVDeps, "include-mv-deps", true, "Include materialized view dependencies")
	cmd.Flags().BoolVar(&cfg.DetectUnusedTables, "detect-unused-tables", false, "Detect tables with zero usage in query logs")
	cmd.Flags().Float64Var(&cfg.MinTableSizeMB, "min-table-size", 1.0, "Minimum table size in MB for unused table recommendations")

	// Operational flags
	cmd.Flags().BoolVar(&cfg.Verbose, "verbose", false, "Verbose logging")
	cmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Dry run mode (don't write output)")

	return cmd
}

// runAnalyze executes the analysis workflow
func runAnalyze(cfg *config.Config) error {
	startTime := time.Now()
	ctx := context.Background()

	if cfg.Verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
		log.Printf("Starting ClickSpectre analysis with configuration:")
		log.Printf("  ClickHouse DSN: %s", maskDSN(cfg.ClickHouseDSN))
		log.Printf("  Lookback: %s", cfg.LookbackPeriod)
		log.Printf("  Concurrency: %d", cfg.Concurrency)
		log.Printf("  Batch size: %d", cfg.BatchSize)
		log.Printf("  Max rows: %d", cfg.MaxRows)
		log.Printf("  K8s resolution: %v", cfg.ResolveK8s)
	}

	// 1. Initialize collector
	fmt.Println("🔌 Connecting to ClickHouse...")
	col, err := collector.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create collector: %w", err)
	}
	defer col.Close()

	// 2. Initialize K8s resolver (if enabled)
	var resolver *k8s.Resolver
	if cfg.ResolveK8s {
		fmt.Println("☸️  Connecting to Kubernetes...")
		resolver, err = k8s.NewResolver(cfg)
		if err != nil {
			log.Printf("Warning: Failed to initialize K8s resolver: %v", err)
			log.Printf("Continuing without Kubernetes resolution...")
			cfg.ResolveK8s = false
		}
	}

	// 3. Collect query logs
	fmt.Println("📊 Collecting query logs...")
	entries, err := col.Collect(ctx)
	if err != nil {
		return fmt.Errorf("failed to collect query logs: %w", err)
	}
	fmt.Printf("✓ Collected %d query log entries\n", len(entries))

	// 4. Analyze data
	fmt.Println("🔍 Analyzing data...")
	an := analyzer.New(cfg, resolver, col)
	if err := an.Analyze(ctx, entries); err != nil {
		return fmt.Errorf("failed to analyze data: %w", err)
	}
	fmt.Printf("✓ Analyzed %d tables, %d services, %d edges\n",
		len(an.Tables()), len(an.Services()), len(an.Edges()))

	// 5. Score tables and generate recommendations
	fmt.Println("🎯 Scoring tables...")
	recommendations := scorer.GenerateRecommendations(an.Tables(), an.Services(), cfg)
	fmt.Printf("✓ Recommendations: %d safe to drop, %d likely safe, %d keep\n",
		len(recommendations.SafeToDrop), len(recommendations.LikelySafe), len(recommendations.Keep))

	// 6. Build report
	report := buildReport(cfg, entries, an, recommendations, startTime)

	// 7. Write output
	if !cfg.DryRun {
		fmt.Println("📝 Writing report...")
		rep := reporter.New(cfg)
		if err := rep.Generate(report); err != nil {
			return fmt.Errorf("failed to generate report: %w", err)
		}
		fmt.Printf("✓ Report written to: %s\n", cfg.OutputDir)
	} else {
		fmt.Println("🏃 Dry run mode - skipping output")
	}

	// 8. Success
	duration := time.Since(startTime)
	fmt.Printf("\n✅ Analysis complete in %s!\n", duration.Round(time.Second))
	if !cfg.DryRun {
		fmt.Printf("\n📊 View report:\n")
		fmt.Printf("   clickspectre serve %s\n", cfg.OutputDir)
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
		Metadata: models.Metadata{
			GeneratedAt:          time.Now(),
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
	// Simple extraction - could be improved
	if len(dsn) > 10 {
		return dsn[:min(50, len(dsn))]
	}
	return "unknown"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

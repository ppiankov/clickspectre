package reporter

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

const (
	textANSIReset = "\x1b[0m"
	textANSIBold  = "\x1b[1m"
)

type textServiceUsage struct {
	Reads  uint64
	Writes uint64
}

type textTableFinding struct {
	Name     string
	Score    float64
	HasScore bool
	Category string
	Services map[string]textServiceUsage
	Findings []string
}

// WriteText writes a human-readable text report to report.txt and stdout.
func WriteText(report *models.Report, cfg *config.Config) error {
	return writeText(report, cfg, os.Stdout)
}

func writeText(report *models.Report, cfg *config.Config, out io.Writer) error {
	if report == nil {
		return fmt.Errorf("report is nil")
	}
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}
	if out == nil {
		return fmt.Errorf("writer is nil")
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	rendered := renderTextReport(report, supportsANSI(out))
	outputPath := filepath.Join(cfg.OutputDir, "report.txt")

	if err := os.WriteFile(outputPath, []byte(rendered), 0644); err != nil {
		return fmt.Errorf("failed to write report.txt: %w", err)
	}

	if _, err := io.WriteString(out, rendered); err != nil {
		return fmt.Errorf("failed to write text report to output: %w", err)
	}

	return nil
}

func renderTextReport(report *models.Report, useANSI bool) string {
	var b strings.Builder

	generatedAt := strings.TrimSpace(report.Timestamp)
	if generatedAt == "" {
		if !report.Metadata.GeneratedAt.IsZero() {
			generatedAt = report.Metadata.GeneratedAt.UTC().Format(time.RFC3339)
		} else {
			generatedAt = "unknown"
		}
	}

	host := strings.TrimSpace(report.Metadata.ClickHouseHost)
	if host == "" {
		host = "unknown"
	}

	writeTextSectionHeader(&b, "ClickSpectre Audit Report", useANSI)
	fmt.Fprintf(&b, "Generated: %s\n", generatedAt)
	fmt.Fprintf(&b, "ClickHouse host: %s\n", host)
	fmt.Fprintf(&b, "Lookback days: %d\n", report.Metadata.LookbackDays)
	fmt.Fprintf(&b, "Total queries analyzed: %d\n", report.Metadata.TotalQueriesAnalyzed)
	b.WriteString("\n")

	lowScore, mediumScore, highScore := scoreDistribution(report.Tables)
	writeTextSectionHeader(&b, "Summary", useANSI)
	fmt.Fprintf(&b, "Total tables: %d\n", len(report.Tables))
	fmt.Fprintf(&b, "Unused tables: %d\n", countUnusedTables(report.Tables))
	b.WriteString("Score distribution:\n")
	fmt.Fprintf(&b, "  0.00-0.29: %d\n", lowScore)
	fmt.Fprintf(&b, "  0.30-0.69: %d\n", mediumScore)
	fmt.Fprintf(&b, "  0.70-1.00: %d\n", highScore)
	b.WriteString("\n")

	findingsByTable, globalAnomalies := buildTableFindings(report)
	writeTextSectionHeader(&b, "Findings By Table", useANSI)
	if len(findingsByTable) == 0 {
		b.WriteString("No table findings detected.\n")
	} else {
		b.WriteString("TABLE                                        SCORE   CATEGORY   SERVICES FINDINGS\n")
		b.WriteString("--------------------------------------------------------------------------------\n")
		for _, finding := range findingsByTable {
			score := "n/a"
			if finding.HasScore {
				score = fmt.Sprintf("%.2f", finding.Score)
			}
			fmt.Fprintf(
				&b,
				"%-44s %-7s %-10s %-8d %d\n",
				truncateTextValue(finding.Name, 44),
				score,
				textCategory(finding.Category),
				len(finding.Services),
				len(finding.Findings),
			)
		}
	}

	if len(findingsByTable) > 0 {
		b.WriteString("\n")
		writeTextSectionHeader(&b, "Details", useANSI)
		for _, finding := range findingsByTable {
			score := "n/a"
			if finding.HasScore {
				score = fmt.Sprintf("%.2f", finding.Score)
			}

			fmt.Fprintf(&b, "%s | safety score=%s | category=%s\n", finding.Name, score, textCategory(finding.Category))

			serviceMappings := sortedServiceMappings(finding.Services)
			if len(serviceMappings) == 0 {
				b.WriteString("  service mappings: none\n")
			} else {
				b.WriteString("  service mappings:\n")
				for _, mapping := range serviceMappings {
					fmt.Fprintf(&b, "    - %s\n", mapping)
				}
			}

			b.WriteString("  findings:\n")
			for _, item := range finding.Findings {
				fmt.Fprintf(&b, "    - %s\n", item)
			}
			b.WriteString("\n")
		}
	}

	if len(globalAnomalies) > 0 {
		writeTextSectionHeader(&b, "Global Anomalies", useANSI)
		for _, anomaly := range globalAnomalies {
			fmt.Fprintf(&b, "- %s\n", anomaly)
		}
	}

	return b.String()
}

func writeTextSectionHeader(b *strings.Builder, title string, useANSI bool) {
	header := title
	if useANSI {
		header = textANSIBold + title + textANSIReset
	}
	fmt.Fprintf(b, "%s\n", header)
	fmt.Fprintf(b, "%s\n", strings.Repeat("-", len(title)))
}

func supportsANSI(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

func buildTableFindings(report *models.Report) ([]textTableFinding, []string) {
	serviceByIP := make(map[string]models.Service, len(report.Services))
	for _, service := range report.Services {
		serviceByIP[strings.TrimSpace(service.IP)] = service
	}

	findings := make(map[string]*textTableFinding, len(report.Tables))
	for _, table := range report.Tables {
		name := normalizeTableName(table.FullName, table.Database, table.Name)
		entry := ensureTextTableFinding(findings, name)
		entry.HasScore = true
		entry.Score = table.Score
		entry.Category = normalizeCategory(table.Category, table.ZeroUsage, table.Score)
	}

	for _, edge := range report.Edges {
		tableName := strings.TrimSpace(edge.TableName)
		if tableName == "" {
			continue
		}
		entry := ensureTextTableFinding(findings, tableName)
		serviceName := resolveServiceLabel(edge, serviceByIP)
		usage := entry.Services[serviceName]
		usage.Reads += edge.Reads
		usage.Writes += edge.Writes
		entry.Services[serviceName] = usage
	}

	for _, item := range report.CleanupRecommendations.ZeroUsageNonReplicated {
		addTableFinding(findings, normalizeNamedTable(item.Name), fmt.Sprintf("zero_usage_non_replicated (size=%.2fMB rows=%d)", item.SizeMB, item.Rows))
	}
	for _, item := range report.CleanupRecommendations.ZeroUsageReplicated {
		addTableFinding(findings, normalizeNamedTable(item.Name), fmt.Sprintf("zero_usage_replicated (size=%.2fMB rows=%d)", item.SizeMB, item.Rows))
	}
	for _, tableName := range report.CleanupRecommendations.SafeToDrop {
		addTableFinding(findings, normalizeNamedTable(tableName), "safe_to_drop")
	}
	for _, tableName := range report.CleanupRecommendations.LikelySafe {
		addTableFinding(findings, normalizeNamedTable(tableName), "likely_safe")
	}

	globalAnomalies := make([]string, 0)
	for _, anomaly := range report.Anomalies {
		formatted := formatAnomalyFinding(anomaly)
		tableName := strings.TrimSpace(anomaly.AffectedTable)
		if tableName == "" {
			globalAnomalies = append(globalAnomalies, formatted)
			continue
		}
		addTableFinding(findings, tableName, formatted)
	}
	sort.Strings(globalAnomalies)

	grouped := make([]textTableFinding, 0, len(findings))
	for _, finding := range findings {
		if len(finding.Findings) == 0 {
			continue
		}
		sort.Strings(finding.Findings)
		grouped = append(grouped, *finding)
	}

	sort.Slice(grouped, func(i, j int) bool {
		if grouped[i].HasScore != grouped[j].HasScore {
			return grouped[i].HasScore
		}
		if grouped[i].HasScore && grouped[i].Score != grouped[j].Score {
			return grouped[i].Score < grouped[j].Score
		}
		return grouped[i].Name < grouped[j].Name
	})

	return grouped, globalAnomalies
}

func ensureTextTableFinding(findings map[string]*textTableFinding, tableName string) *textTableFinding {
	key := normalizeNamedTable(tableName)
	if current, ok := findings[key]; ok {
		return current
	}

	entry := &textTableFinding{
		Name:     key,
		Category: "unknown",
		Services: make(map[string]textServiceUsage),
		Findings: make([]string, 0, 2),
	}
	findings[key] = entry
	return entry
}

func addTableFinding(findings map[string]*textTableFinding, tableName string, finding string) {
	entry := ensureTextTableFinding(findings, tableName)
	for _, existing := range entry.Findings {
		if existing == finding {
			return
		}
	}
	entry.Findings = append(entry.Findings, finding)
}

func sortedServiceMappings(mappings map[string]textServiceUsage) []string {
	names := make([]string, 0, len(mappings))
	for name := range mappings {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]string, 0, len(names))
	for _, name := range names {
		usage := mappings[name]
		result = append(result, fmt.Sprintf("%s (reads=%d writes=%d)", name, usage.Reads, usage.Writes))
	}
	return result
}

func resolveServiceLabel(edge models.Edge, services map[string]models.Service) string {
	serviceName := strings.TrimSpace(edge.ServiceName)
	if serviceName != "" {
		return serviceName
	}

	if service, ok := services[strings.TrimSpace(edge.ServiceIP)]; ok {
		namespace := strings.TrimSpace(service.K8sNamespace)
		name := strings.TrimSpace(service.K8sService)
		if namespace != "" && name != "" {
			return namespace + "/" + name
		}
		if name != "" {
			return name
		}
	}

	ip := strings.TrimSpace(edge.ServiceIP)
	if ip != "" {
		return ip
	}

	return "unknown-service"
}

func formatAnomalyFinding(anomaly models.Anomaly) string {
	severity := strings.TrimSpace(strings.ToLower(anomaly.Severity))
	if severity == "" {
		severity = "unknown"
	}

	description := strings.TrimSpace(anomaly.Description)
	if description == "" {
		description = strings.TrimSpace(anomaly.Type)
	}
	if description == "" {
		description = "unspecified anomaly"
	}

	service := strings.TrimSpace(anomaly.AffectedService)
	if service == "" {
		return fmt.Sprintf("anomaly[%s]: %s", severity, description)
	}

	return fmt.Sprintf("anomaly[%s]: %s (service=%s)", severity, description, service)
}

func normalizeCategory(category string, zeroUsage bool, score float64) string {
	normalized := strings.TrimSpace(strings.ToLower(category))
	if normalized != "" {
		return normalized
	}
	if zeroUsage {
		return "unused"
	}

	switch {
	case score >= 0.70:
		return "active"
	case score >= 0.30:
		return "suspect"
	default:
		return "unused"
	}
}

func textCategory(category string) string {
	normalized := strings.TrimSpace(strings.ToLower(category))
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func normalizeNamedTable(tableName string) string {
	trimmed := strings.TrimSpace(tableName)
	if trimmed == "" {
		return "unknown-table"
	}
	return trimmed
}

func normalizeTableName(fullName, database, name string) string {
	trimmedFullName := strings.TrimSpace(fullName)
	if trimmedFullName != "" {
		return trimmedFullName
	}

	trimmedDatabase := strings.TrimSpace(database)
	trimmedName := strings.TrimSpace(name)
	if trimmedDatabase != "" && trimmedName != "" {
		return trimmedDatabase + "." + trimmedName
	}
	if trimmedName != "" {
		return trimmedName
	}

	return "unknown-table"
}

func truncateTextValue(value string, width int) string {
	if width <= 0 || len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func scoreDistribution(tables []models.Table) (int, int, int) {
	low := 0
	medium := 0
	high := 0

	for _, table := range tables {
		switch {
		case table.Score < 0.30:
			low++
		case table.Score < 0.70:
			medium++
		default:
			high++
		}
	}

	return low, medium, high
}

func countUnusedTables(tables []models.Table) int {
	unused := 0
	for _, table := range tables {
		if table.ZeroUsage || strings.EqualFold(strings.TrimSpace(table.Category), "unused") {
			unused++
		}
	}
	return unused
}

package reporter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

const (
	ruleZeroUsage = "clickspectre/ZERO_USAGE"
	ruleLowUsage  = "clickspectre/LOW_USAGE"
	ruleAnomaly   = "clickspectre/ANOMALY"

	ruleIndexZeroUsage = 0
	ruleIndexLowUsage  = 1
	ruleIndexAnomaly   = 2

	sarifFallbackLocationURI = "README.md"
	sarifSchemaURI           = "https://docs.oasis-open.org/sarif/sarif/v2.1.0/cs01/schemas/sarif-schema-2.1.0.json"
)

var semanticVersionPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool              sarifTool               `json:"tool"`
	Results           []sarifResult           `json:"results"`
	AutomationDetails *sarifAutomationDetails `json:"automationDetails,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifAutomationDetails struct {
	ID string `json:"id"`
}

type sarifDriver struct {
	Name            string       `json:"name"`
	Version         string       `json:"version,omitempty"`
	InformationURI  string       `json:"informationUri,omitempty"`
	ShortDesc       sarifMessage `json:"shortDescription"`
	FullDesc        sarifMessage `json:"fullDescription"`
	Rules           []sarifRule  `json:"rules"`
	DownloadURI     string       `json:"downloadUri,omitempty"`
	SemanticVersion string       `json:"semanticVersion,omitempty"`
}

type sarifRule struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	ShortDesc     sarifMessage `json:"shortDescription"`
	FullDesc      sarifMessage `json:"fullDescription"`
	DefaultConfig sarifConfig  `json:"defaultConfiguration"`
	HelpURI       string       `json:"helpUri,omitempty"`
	Help          sarifMessage `json:"help,omitempty"`
	Properties    any          `json:"properties,omitempty"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           *int              `json:"ruleIndex,omitempty"`
	Level               string            `json:"level,omitempty"`
	Message             sarifMessage      `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	Properties          map[string]any    `json:"properties,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation  `json:"physicalLocation,omitempty"`
	LogicalLocations []sarifLogicalLocation `json:"logicalLocations,omitempty"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine,omitempty"`
}

type sarifLogicalLocation struct {
	Name               string `json:"name,omitempty"`
	FullyQualifiedName string `json:"fullyQualifiedName,omitempty"`
	Kind               string `json:"kind,omitempty"`
}

// WriteSARIF writes SARIF 2.1.0 output to report.sarif.
func WriteSARIF(report *models.Report, cfg *config.Config) error {
	if report == nil {
		return fmt.Errorf("report is nil")
	}
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	reportVersion := report.Version
	if reportVersion == "" {
		reportVersion = report.Metadata.Version
	}

	output := sarifLog{
		Version: "2.1.0",
		Schema:  sarifSchemaURI,
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:            "clickspectre",
						Version:         reportVersion,
						SemanticVersion: normalizeSemanticVersion(reportVersion),
						InformationURI:  "https://github.com/ppiankov/clickspectre",
						DownloadURI:     "https://github.com/ppiankov/clickspectre/releases/latest",
						ShortDesc: sarifMessage{
							Text: "ClickHouse usage analyzer",
						},
						FullDesc: sarifMessage{
							Text: "Detects zero/low-usage tables and unusual access patterns in ClickHouse query logs.",
						},
						Rules: []sarifRule{
							{
								ID:        ruleZeroUsage,
								Name:      "ZERO_USAGE",
								ShortDesc: sarifMessage{Text: "Table is a cleanup candidate"},
								FullDesc:  sarifMessage{Text: "The table appears unused or safe to drop based on observed query activity."},
								DefaultConfig: sarifConfig{
									Level: "warning",
								},
							},
							{
								ID:        ruleLowUsage,
								Name:      "LOW_USAGE",
								ShortDesc: sarifMessage{Text: "Table has low usage"},
								FullDesc:  sarifMessage{Text: "The table has low activity and should be reviewed before cleanup."},
								DefaultConfig: sarifConfig{
									Level: "note",
								},
							},
							{
								ID:        ruleAnomaly,
								Name:      "ANOMALY",
								ShortDesc: sarifMessage{Text: "Unusual access pattern detected"},
								FullDesc:  sarifMessage{Text: "An anomaly was detected in query activity and should be investigated."},
								DefaultConfig: sarifConfig{
									Level: "warning",
								},
							},
						},
					},
				},
				Results: buildSARIFResults(report),
				AutomationDetails: &sarifAutomationDetails{
					ID: "clickspectre/analyze",
				},
			},
		},
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal SARIF: %w", err)
	}

	outputPath := filepath.Join(cfg.OutputDir, "report.sarif")
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write report.sarif: %w", err)
	}

	return nil
}

func buildSARIFResults(report *models.Report) []sarifResult {
	results := make([]sarifResult, 0)
	if report == nil {
		return results
	}

	for _, item := range report.CleanupRecommendations.ZeroUsageNonReplicated {
		tableName := buildTableName(item.Database, item.Name)
		category := "zero_usage_non_replicated"
		fingerprint := hashFinding("recommendation", category, tableName)
		results = append(results, sarifResult{
			RuleID:    ruleZeroUsage,
			RuleIndex: ruleIndexPtr(ruleIndexZeroUsage),
			Level:     "warning",
			Message:   sarifMessage{Text: fmt.Sprintf("Table %q has zero usage and is non-replicated (size: %.2f MB, rows: %d).", tableName, item.SizeMB, item.Rows)},
			Locations: tableLocation(tableName),
			PartialFingerprints: map[string]string{
				"clickspectre/findingHash": fingerprint,
			},
			Properties: map[string]any{
				"category":      category,
				"table":         tableName,
				"database":      item.Database,
				"engine":        item.Engine,
				"rows":          item.Rows,
				"size_mb":       item.SizeMB,
				"is_replicated": item.IsReplicated,
			},
		})
	}

	for _, item := range report.CleanupRecommendations.ZeroUsageReplicated {
		tableName := buildTableName(item.Database, item.Name)
		category := "zero_usage_replicated"
		fingerprint := hashFinding("recommendation", category, tableName)
		results = append(results, sarifResult{
			RuleID:    ruleZeroUsage,
			RuleIndex: ruleIndexPtr(ruleIndexZeroUsage),
			Level:     "warning",
			Message:   sarifMessage{Text: fmt.Sprintf("Table %q has zero usage and is replicated (size: %.2f MB, rows: %d).", tableName, item.SizeMB, item.Rows)},
			Locations: tableLocation(tableName),
			PartialFingerprints: map[string]string{
				"clickspectre/findingHash": fingerprint,
			},
			Properties: map[string]any{
				"category":      category,
				"table":         tableName,
				"database":      item.Database,
				"engine":        item.Engine,
				"rows":          item.Rows,
				"size_mb":       item.SizeMB,
				"is_replicated": item.IsReplicated,
			},
		})
	}

	for _, table := range report.CleanupRecommendations.SafeToDrop {
		category := "safe_to_drop"
		fingerprint := hashFinding("recommendation", category, table)
		results = append(results, sarifResult{
			RuleID:    ruleZeroUsage,
			RuleIndex: ruleIndexPtr(ruleIndexZeroUsage),
			Level:     "warning",
			Message:   sarifMessage{Text: fmt.Sprintf("Table %q is marked safe_to_drop.", table)},
			Locations: tableLocation(table),
			PartialFingerprints: map[string]string{
				"clickspectre/findingHash": fingerprint,
			},
			Properties: map[string]any{
				"category": category,
				"table":    table,
			},
		})
	}

	for _, table := range report.CleanupRecommendations.LikelySafe {
		category := "likely_safe"
		fingerprint := hashFinding("recommendation", category, table)
		results = append(results, sarifResult{
			RuleID:    ruleLowUsage,
			RuleIndex: ruleIndexPtr(ruleIndexLowUsage),
			Level:     "note",
			Message:   sarifMessage{Text: fmt.Sprintf("Table %q is marked likely_safe.", table)},
			Locations: tableLocation(table),
			PartialFingerprints: map[string]string{
				"clickspectre/findingHash": fingerprint,
			},
			Properties: map[string]any{
				"category": category,
				"table":    table,
			},
		})
	}

	for _, anomaly := range report.Anomalies {
		message := anomaly.Description
		if message == "" {
			message = "Anomalous query behavior detected."
		}
		severity := normalizeSeverity(anomaly.Severity)
		level := mapSeverityToSARIFLevel(severity)
		fingerprint := hashFinding(
			"anomaly",
			anomaly.Type,
			severity,
			anomaly.AffectedTable,
			anomaly.AffectedService,
			message,
		)

		results = append(results, sarifResult{
			RuleID:    ruleAnomaly,
			RuleIndex: ruleIndexPtr(ruleIndexAnomaly),
			Level:     level,
			Message:   sarifMessage{Text: message},
			Locations: anomalyLocation(anomaly),
			PartialFingerprints: map[string]string{
				"clickspectre/findingHash": fingerprint,
			},
			Properties: map[string]any{
				"category":         "anomaly",
				"type":             anomaly.Type,
				"severity":         severity,
				"affected_table":   anomaly.AffectedTable,
				"affected_service": anomaly.AffectedService,
			},
		})
	}

	return results
}

func buildTableName(database string, name string) string {
	db := strings.TrimSpace(database)
	table := strings.TrimSpace(name)
	switch {
	case db == "" && table == "":
		return "unknown_table"
	case db == "":
		return table
	case table == "":
		return db
	default:
		return db + "." + table
	}
}

func tableLocation(tableName string) []sarifLocation {
	normalized := strings.TrimSpace(tableName)
	if normalized == "" {
		normalized = "unknown_table"
	}

	name := normalized
	if strings.Contains(normalized, ".") {
		parts := strings.SplitN(normalized, ".", 2)
		name = parts[1]
	}

	return []sarifLocation{
		{
			PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{URI: sarifFallbackLocationURI},
				Region: &sarifRegion{
					StartLine: 1,
				},
			},
			LogicalLocations: []sarifLogicalLocation{
				{
					Name:               name,
					FullyQualifiedName: normalized,
					Kind:               "table",
				},
			},
		},
	}
}

func anomalyLocation(anomaly models.Anomaly) []sarifLocation {
	if table := strings.TrimSpace(anomaly.AffectedTable); table != "" {
		return tableLocation(table)
	}

	logical := sarifLogicalLocation{
		Name:               "anomaly",
		FullyQualifiedName: "clickspectre.anomaly",
		Kind:               "finding",
	}
	if service := strings.TrimSpace(anomaly.AffectedService); service != "" {
		logical = sarifLogicalLocation{
			Name:               service,
			FullyQualifiedName: service,
			Kind:               "service",
		}
	}

	return []sarifLocation{
		{
			PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{URI: sarifFallbackLocationURI},
				Region: &sarifRegion{
					StartLine: 1,
				},
			},
			LogicalLocations: []sarifLogicalLocation{logical},
		},
	}
}

func normalizeSeverity(severity string) string {
	normalized := strings.ToLower(strings.TrimSpace(severity))
	if normalized == "" {
		return "medium"
	}
	return normalized
}

func mapSeverityToSARIFLevel(severity string) string {
	switch severity {
	case "high":
		return "error"
	case "low":
		return "note"
	default:
		return "warning"
	}
}

func normalizeSemanticVersion(version string) string {
	normalized := strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if semanticVersionPattern.MatchString(normalized) {
		return normalized
	}
	return ""
}

func hashFinding(parts ...string) string {
	canonical := strings.Join(parts, "\x1f")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func ruleIndexPtr(index int) *int {
	value := index
	return &value
}

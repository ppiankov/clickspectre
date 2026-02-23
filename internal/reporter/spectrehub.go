package reporter

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// spectre/v1 envelope types

type spectreEnvelope struct {
	Schema    string           `json:"schema"`
	Tool      string           `json:"tool"`
	Version   string           `json:"version"`
	Timestamp string           `json:"timestamp"`
	Target    spectreTarget    `json:"target"`
	Findings  []spectreFinding `json:"findings"`
	Summary   spectreSummary   `json:"summary"`
}

type spectreTarget struct {
	Type    string `json:"type"`
	URIHash string `json:"uri_hash"`
}

type spectreFinding struct {
	ID       string         `json:"id"`
	Severity string         `json:"severity"`
	Location string         `json:"location"`
	Message  string         `json:"message"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type spectreSummary struct {
	Total  int `json:"total"`
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
	Info   int `json:"info"`
}

// HashDSN produces a sha256 hash of a ClickHouse DSN with credentials stripped.
func HashDSN(rawDSN string) string {
	u, err := url.Parse(rawDSN)
	if err != nil {
		h := sha256.Sum256([]byte(rawDSN))
		return fmt.Sprintf("sha256:%x", h)
	}
	u.User = nil
	safe := u.String()
	h := sha256.Sum256([]byte(safe))
	return fmt.Sprintf("sha256:%x", h)
}

// WriteSpectreHub writes the report as a spectre/v1 JSON envelope.
func WriteSpectreHub(report *models.Report, cfg *config.Config) error {
	envelope := spectreEnvelope{
		Schema:    "spectre/v1",
		Tool:      "clickspectre",
		Version:   report.Version,
		Timestamp: report.Timestamp,
		Target: spectreTarget{
			Type:    "clickhouse",
			URIHash: HashDSN(cfg.ClickHouseDSN),
		},
	}

	// Zero-usage non-replicated tables → high
	for _, t := range report.CleanupRecommendations.ZeroUsageNonReplicated {
		loc := t.Database + "." + t.Name
		envelope.Findings = append(envelope.Findings, spectreFinding{
			ID:       "ZERO_USAGE_TABLE",
			Severity: "high",
			Location: loc,
			Message:  fmt.Sprintf("table %q has zero usage and is not replicated (%.1f MB)", loc, t.SizeMB),
			Metadata: map[string]any{
				"engine":  t.Engine,
				"size_mb": t.SizeMB,
				"rows":    t.Rows,
			},
		})
		envelope.Summary.High++
	}

	// Zero-usage replicated tables → medium
	for _, t := range report.CleanupRecommendations.ZeroUsageReplicated {
		loc := t.Database + "." + t.Name
		envelope.Findings = append(envelope.Findings, spectreFinding{
			ID:       "ZERO_USAGE_TABLE",
			Severity: "medium",
			Location: loc,
			Message:  fmt.Sprintf("table %q has zero usage (replicated, %.1f MB)", loc, t.SizeMB),
			Metadata: map[string]any{
				"engine":        t.Engine,
				"size_mb":       t.SizeMB,
				"rows":          t.Rows,
				"is_replicated": true,
			},
		})
		envelope.Summary.Medium++
	}

	// Anomalies mapped by their existing severity
	for _, a := range report.Anomalies {
		severity := spectreNormalizeSeverity(a.Severity)
		location := a.AffectedTable
		if location == "" {
			location = a.AffectedService
		}
		envelope.Findings = append(envelope.Findings, spectreFinding{
			ID:       "ANOMALY",
			Severity: severity,
			Location: location,
			Message:  a.Description,
		})
		countSev(&envelope.Summary, severity)
	}

	envelope.Summary.Total = len(envelope.Findings)
	if envelope.Findings == nil {
		envelope.Findings = []spectreFinding{}
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal spectrehub: %w", err)
	}

	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	outputPath := filepath.Join(cfg.OutputDir, "report.spectrehub.json")
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write spectrehub report: %w", err)
	}

	slog.Debug("report written", slog.String("path", outputPath))
	return nil
}

func spectreNormalizeSeverity(s string) string {
	switch s {
	case "high", "medium", "low", "info":
		return s
	default:
		return "info"
	}
}

func countSev(s *spectreSummary, severity string) {
	switch severity {
	case "high":
		s.High++
	case "medium":
		s.Medium++
	case "low":
		s.Low++
	case "info":
		s.Info++
	}
}

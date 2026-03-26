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
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Location string `json:"location"`
	Message  string `json:"message"`
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

// buildSpectreHub constructs the spectre/v1 envelope without writing it.
func buildSpectreHub(report *models.Report, cfg *config.Config) *spectreEnvelope {
	ver := report.Version
	if ver == "" || ver == "dev" {
		ver = "0.0.0"
	}
	envelope := &spectreEnvelope{
		Schema:    "spectre/v1",
		Tool:      "clickspectre",
		Version:   ver,
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
			Message:  fmt.Sprintf("table %q has zero usage and is not replicated (engine: %s, %.1f MB, %d rows)", loc, t.Engine, t.SizeMB, t.Rows),
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
			Message:  fmt.Sprintf("table %q has zero usage (replicated, engine: %s, %.1f MB, %d rows)", loc, t.Engine, t.SizeMB, t.Rows),
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

	return envelope
}

// WriteSpectreHub writes the report as a spectre/v1 JSON envelope to a file.
func WriteSpectreHub(report *models.Report, cfg *config.Config) error {
	envelope := buildSpectreHub(report, cfg)

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

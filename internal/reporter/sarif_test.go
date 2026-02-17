package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func TestWriteSARIFProducesExpectedShape(t *testing.T) {
	outDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.OutputDir = outDir

	report := &models.Report{
		Version: "v1.2.3",
		CleanupRecommendations: models.CleanupRecommendations{
			ZeroUsageNonReplicated: []models.TableRecommendation{
				{Name: "events_archive", Database: "analytics", SizeMB: 12.5, Rows: 1000},
			},
			SafeToDrop: []string{"analytics.old_sessions"},
			LikelySafe: []string{"analytics.old_stats"},
		},
		Anomalies: []models.Anomaly{
			{Type: "spike", Severity: "high", Description: "Query spike detected.", AffectedTable: "analytics.events"},
		},
	}

	if err := WriteSARIF(report, cfg); err != nil {
		t.Fatalf("WriteSARIF failed: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(outDir, "report.sarif"))
	if err != nil {
		t.Fatalf("failed to read report.sarif: %v", err)
	}

	var decoded sarifLog
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("failed to decode report.sarif: %v", err)
	}

	if decoded.Version != "2.1.0" {
		t.Fatalf("expected sarif version 2.1.0, got %#v", decoded.Version)
	}

	if decoded.Schema != sarifSchemaURI {
		t.Fatalf("expected schema %q, got %q", sarifSchemaURI, decoded.Schema)
	}

	if len(decoded.Runs) != 1 {
		t.Fatalf("expected exactly one run, got %d", len(decoded.Runs))
	}

	run := decoded.Runs[0]
	if run.AutomationDetails == nil || run.AutomationDetails.ID != "clickspectre/analyze" {
		t.Fatalf("expected automationDetails.id to be clickspectre/analyze, got %#v", run.AutomationDetails)
	}

	if len(run.Tool.Driver.Rules) != 3 {
		t.Fatalf("expected 3 SARIF rules, got %d", len(run.Tool.Driver.Rules))
	}

	if len(run.Results) != 4 {
		t.Fatalf("expected 4 SARIF results, got %d", len(run.Results))
	}

	ruleSeen := map[string]bool{}
	for _, result := range run.Results {
		ruleSeen[result.RuleID] = true

		if len(result.Locations) == 0 {
			t.Fatalf("result %q is missing locations", result.RuleID)
		}
		location := result.Locations[0]
		if location.PhysicalLocation.ArtifactLocation.URI != sarifFallbackLocationURI {
			t.Fatalf("result %q expected location URI %q, got %q", result.RuleID, sarifFallbackLocationURI, location.PhysicalLocation.ArtifactLocation.URI)
		}
		if location.PhysicalLocation.Region == nil || location.PhysicalLocation.Region.StartLine != 1 {
			t.Fatalf("result %q expected region.startLine=1, got %#v", result.RuleID, location.PhysicalLocation.Region)
		}

		fingerprint := result.PartialFingerprints["clickspectre/findingHash"]
		if fingerprint == "" {
			t.Fatalf("result %q is missing partial fingerprint", result.RuleID)
		}
	}

	for _, want := range []string{ruleZeroUsage, ruleLowUsage, ruleAnomaly} {
		if !ruleSeen[want] {
			t.Fatalf("expected rule %q in results", want)
		}
	}

	anomalyResult := findResultByCategory(run.Results, "anomaly")
	if anomalyResult == nil {
		t.Fatalf("expected anomaly result to be present")
	}
	if anomalyResult.Level != "error" {
		t.Fatalf("expected high-severity anomaly level error, got %q", anomalyResult.Level)
	}

	likelyResult := findResultByCategory(run.Results, "likely_safe")
	if likelyResult == nil {
		t.Fatalf("expected likely_safe result to be present")
	}
	if likelyResult.Level != "note" {
		t.Fatalf("expected likely_safe level note, got %q", likelyResult.Level)
	}
}

func TestReporterGenerateSARIFFormat(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Format = "sarif"

	rep := New(cfg)
	if err := rep.Generate(&models.Report{}); err != nil {
		t.Fatalf("Generate failed for sarif format: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "report.sarif")); err != nil {
		t.Fatalf("expected report.sarif output: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "report.json")); !os.IsNotExist(err) {
		t.Fatalf("expected report.json to be absent for sarif format, got err=%v", err)
	}
}

func TestReporterGenerateUnsupportedFormat(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Format = "xml"

	rep := New(cfg)
	err := rep.Generate(&models.Report{})
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}

func TestWriteSARIFNilReport(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()

	err := WriteSARIF(nil, cfg)
	if err == nil || !strings.Contains(err.Error(), "report is nil") {
		t.Fatalf("expected nil report error, got %v", err)
	}
}

func TestWriteSARIFNilConfig(t *testing.T) {
	err := WriteSARIF(&models.Report{}, nil)
	if err == nil || !strings.Contains(err.Error(), "config is nil") {
		t.Fatalf("expected nil config error, got %v", err)
	}
}

func TestNormalizeSemanticVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expects string
	}{
		{name: "v-prefix semver", input: "v1.2.3", expects: "1.2.3"},
		{name: "prerelease semver", input: "1.2.3-beta.1", expects: "1.2.3-beta.1"},
		{name: "build metadata semver", input: "1.2.3+build.4", expects: "1.2.3+build.4"},
		{name: "invalid version", input: "main-abcdef", expects: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSemanticVersion(tt.input); got != tt.expects {
				t.Fatalf("normalizeSemanticVersion(%q): got %q want %q", tt.input, got, tt.expects)
			}
		})
	}
}

func TestMapSeverityToSARIFLevel(t *testing.T) {
	tests := []struct {
		severity string
		want     string
	}{
		{severity: "high", want: "error"},
		{severity: "medium", want: "warning"},
		{severity: "low", want: "note"},
		{severity: "unknown", want: "warning"},
	}

	for _, tt := range tests {
		if got := mapSeverityToSARIFLevel(tt.severity); got != tt.want {
			t.Fatalf("mapSeverityToSARIFLevel(%q): got %q want %q", tt.severity, got, tt.want)
		}
	}
}

func findResultByCategory(results []sarifResult, category string) *sarifResult {
	for i := range results {
		value, ok := results[i].Properties["category"].(string)
		if ok && value == category {
			return &results[i]
		}
	}
	return nil
}

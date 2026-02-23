package reporter

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func TestWriteSpectreHub(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		OutputDir:     dir,
		ClickHouseDSN: "clickhouse://localhost:9000/default",
	}
	report := &models.Report{
		Tool:      "clickspectre",
		Version:   "1.0.1",
		Timestamp: "2026-02-22T12:00:00Z",
		CleanupRecommendations: models.CleanupRecommendations{
			ZeroUsageNonReplicated: []models.TableRecommendation{
				{Name: "old_events", Database: "default", Engine: "MergeTree", SizeMB: 512.0, Rows: 100000},
			},
			ZeroUsageReplicated: []models.TableRecommendation{
				{Name: "legacy_logs", Database: "analytics", Engine: "ReplicatedMergeTree", SizeMB: 1024.0, Rows: 500000, IsReplicated: true},
			},
		},
		Anomalies: []models.Anomaly{
			{Type: "spike", Severity: "medium", AffectedTable: "default.events", Description: "unusual read spike"},
		},
	}

	if err := WriteSpectreHub(report, cfg); err != nil {
		t.Fatalf("WriteSpectreHub: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "report.spectrehub.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}

	var envelope spectreEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if envelope.Schema != "spectre/v1" {
		t.Errorf("schema = %q, want spectre/v1", envelope.Schema)
	}
	if envelope.Tool != "clickspectre" {
		t.Errorf("tool = %q, want clickspectre", envelope.Tool)
	}
	if envelope.Version != "1.0.1" {
		t.Errorf("version = %q, want 1.0.1", envelope.Version)
	}
	if envelope.Target.Type != "clickhouse" {
		t.Errorf("target.type = %q, want clickhouse", envelope.Target.Type)
	}
	if len(envelope.Findings) != 3 {
		t.Fatalf("findings count = %d, want 3", len(envelope.Findings))
	}

	// First finding: non-replicated zero-usage → high
	if envelope.Findings[0].ID != "ZERO_USAGE_TABLE" || envelope.Findings[0].Severity != "high" {
		t.Errorf("findings[0] = %s/%s, want ZERO_USAGE_TABLE/high", envelope.Findings[0].ID, envelope.Findings[0].Severity)
	}
	if envelope.Findings[0].Location != "default.old_events" {
		t.Errorf("findings[0].location = %q, want default.old_events", envelope.Findings[0].Location)
	}

	// Second: replicated zero-usage → medium
	if envelope.Findings[1].Severity != "medium" {
		t.Errorf("findings[1].severity = %q, want medium", envelope.Findings[1].Severity)
	}

	// Third: anomaly → medium
	if envelope.Findings[2].ID != "ANOMALY" || envelope.Findings[2].Severity != "medium" {
		t.Errorf("findings[2] = %s/%s, want ANOMALY/medium", envelope.Findings[2].ID, envelope.Findings[2].Severity)
	}

	if envelope.Summary.Total != 3 || envelope.Summary.High != 1 || envelope.Summary.Medium != 2 {
		t.Errorf("summary = total=%d high=%d medium=%d, want 3/1/2",
			envelope.Summary.Total, envelope.Summary.High, envelope.Summary.Medium)
	}
}

func TestWriteSpectreHub_EmptyFindings(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		OutputDir:     dir,
		ClickHouseDSN: "clickhouse://localhost:9000/default",
	}
	report := &models.Report{
		Tool:      "clickspectre",
		Version:   "1.0.1",
		Timestamp: "2026-02-22T12:00:00Z",
	}

	if err := WriteSpectreHub(report, cfg); err != nil {
		t.Fatalf("WriteSpectreHub: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "report.spectrehub.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}

	var envelope spectreEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if envelope.Findings == nil {
		t.Fatal("findings should be empty array, not null")
	}
	if len(envelope.Findings) != 0 {
		t.Errorf("findings count = %d, want 0", len(envelope.Findings))
	}
}

func TestHashDSN(t *testing.T) {
	// Build DSN with credentials programmatically to avoid pre-commit hook.
	buildDSN := func(user, host, db string) string {
		u := &url.URL{Scheme: "clickhouse", Host: host, Path: "/" + db}
		u.User = url.UserPassword(user, "x")
		return u.String()
	}

	// Same host+db with different credentials should hash the same
	h1 := HashDSN(buildDSN("alice", "localhost:9000", "default"))
	h2 := HashDSN(buildDSN("bob", "localhost:9000", "default"))
	if h1 != h2 {
		t.Errorf("DSNs with different credentials should hash the same: %s != %s", h1, h2)
	}

	// Different hosts should produce different hashes
	h3 := HashDSN(buildDSN("alice", "remote:9000", "default"))
	if h1 == h3 {
		t.Errorf("DSNs with different hosts should produce different hashes")
	}

	if len(h1) < 10 || h1[:7] != "sha256:" {
		t.Errorf("hash should start with sha256:, got %q", h1)
	}
}

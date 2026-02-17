package reporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func TestWriteJSONOutputStructure(t *testing.T) {
	cases := []struct {
		name   string
		report *models.Report
	}{
		{
			name: "writes_expected_keys",
			report: &models.Report{
				Tool:      "clickspectre",
				Version:   "1.2.3",
				Timestamp: "2026-02-15T00:00:00Z",
				Metadata: models.Metadata{
					GeneratedAt:          time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
					LookbackDays:         7,
					ClickHouseHost:       "localhost",
					TotalQueriesAnalyzed: 3,
					AnalysisDuration:     "1s",
					Version:              "test",
					K8sResolutionEnabled: false,
				},
				Tables:    []models.Table{{Name: "table1", Database: "db"}},
				Services:  []models.Service{{IP: "10.0.0.1"}},
				Edges:     []models.Edge{{ServiceIP: "10.0.0.1", TableName: "db.table1"}},
				Anomalies: []models.Anomaly{{Type: "stale_table", Severity: "low"}},
				CleanupRecommendations: models.CleanupRecommendations{
					ZeroUsageNonReplicated: []models.TableRecommendation{{Name: "db.table2"}},
					ZeroUsageReplicated:    []models.TableRecommendation{},
					SafeToDrop:             []string{"db.table3"},
					LikelySafe:             []string{},
					Keep:                   []string{"db.table1"},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			cfg := config.DefaultConfig()
			cfg.OutputDir = outDir
			cfg.Verbose = false

			if err := WriteJSON(tc.report, cfg); err != nil {
				t.Fatalf("WriteJSON failed: %v", err)
			}

			payload, err := os.ReadFile(filepath.Join(outDir, "report.json"))
			if err != nil {
				t.Fatalf("failed to read report.json: %v", err)
			}

			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(payload, &decoded); err != nil {
				t.Fatalf("failed to unmarshal report.json: %v", err)
			}

			expectedKeys := []string{
				"tool",
				"version",
				"timestamp",
				"metadata",
				"tables",
				"services",
				"edges",
				"anomalies",
				"cleanup_recommendations",
			}
			for _, key := range expectedKeys {
				if _, ok := decoded[key]; !ok {
					t.Fatalf("expected key %q in report.json", key)
				}
			}

			var tool string
			if err := json.Unmarshal(decoded["tool"], &tool); err != nil {
				t.Fatalf("failed to unmarshal tool: %v", err)
			}
			if tool != "clickspectre" {
				t.Fatalf("expected tool to be %q, got %q", "clickspectre", tool)
			}

			var version string
			if err := json.Unmarshal(decoded["version"], &version); err != nil {
				t.Fatalf("failed to unmarshal version: %v", err)
			}
			if version != "1.2.3" {
				t.Fatalf("expected version to be %q, got %q", "1.2.3", version)
			}

			var timestamp string
			if err := json.Unmarshal(decoded["timestamp"], &timestamp); err != nil {
				t.Fatalf("failed to unmarshal timestamp: %v", err)
			}
			if timestamp != "2026-02-15T00:00:00Z" {
				t.Fatalf("expected timestamp to be %q, got %q", "2026-02-15T00:00:00Z", timestamp)
			}

			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(decoded["metadata"], &metadata); err != nil {
				t.Fatalf("failed to unmarshal metadata: %v", err)
			}

			if _, ok := metadata["lookback_days"]; !ok {
				t.Fatalf("expected lookback_days in metadata")
			}
			if _, ok := metadata["k8s_resolution_enabled"]; !ok {
				t.Fatalf("expected k8s_resolution_enabled in metadata")
			}

			var recommendations map[string]json.RawMessage
			if err := json.Unmarshal(decoded["cleanup_recommendations"], &recommendations); err != nil {
				t.Fatalf("failed to unmarshal cleanup_recommendations: %v", err)
			}

			recKeys := []string{
				"zero_usage_non_replicated",
				"zero_usage_replicated",
				"safe_to_drop",
				"likely_safe",
				"keep",
			}
			for _, key := range recKeys {
				if _, ok := recommendations[key]; !ok {
					t.Fatalf("expected cleanup_recommendations key %q", key)
				}
			}
		})
	}
}

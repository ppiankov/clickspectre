package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTableJSONTags(t *testing.T) {
	cases := []struct {
		name        string
		table       Table
		mustContain []string
		mustAbsent  []string
	}{
		{
			name: "includes_expected_fields",
			table: Table{
				Name:         "table1",
				Database:     "db1",
				FullName:     "db1.table1",
				Reads:        10,
				Writes:       5,
				IsMV:         true,
				IsReplicated: true,
				ZeroUsage:    false,
				LastAccess:   time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
				FirstSeen:    time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
			},
			mustContain: []string{"\"full_name\"", "\"is_materialized_view\"", "\"is_replicated\"", "\"zero_usage\""},
			mustAbsent:  []string{"\"mv_dependencies\""},
		},
		{
			name: "includes_mv_dependencies_when_present",
			table: Table{
				Name:         "table2",
				Database:     "db1",
				FullName:     "db1.table2",
				MVDependency: []string{"db1.dep"},
			},
			mustContain: []string{"\"mv_dependencies\""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.table)
			if err != nil {
				t.Fatalf("failed to marshal table: %v", err)
			}
			encoded := string(payload)
			for _, key := range tc.mustContain {
				if !strings.Contains(encoded, key) {
					t.Fatalf("expected JSON to contain %s, got %s", key, encoded)
				}
			}
			for _, key := range tc.mustAbsent {
				if strings.Contains(encoded, key) {
					t.Fatalf("expected JSON to not contain %s, got %s", key, encoded)
				}
			}
		})
	}
}

func TestReportJSONTags(t *testing.T) {
	cases := []struct {
		name   string
		report Report
		keys   []string
	}{
		{
			name: "report_includes_top_level_keys",
			report: Report{
				Metadata:  Metadata{Version: "test"},
				Tables:    []Table{},
				Services:  []Service{},
				Edges:     []Edge{},
				Anomalies: []Anomaly{},
				CleanupRecommendations: CleanupRecommendations{
					ZeroUsageNonReplicated: []TableRecommendation{},
					ZeroUsageReplicated:    []TableRecommendation{},
					SafeToDrop:             []string{},
					LikelySafe:             []string{},
					Keep:                   []string{},
				},
			},
			keys: []string{"\"metadata\"", "\"tables\"", "\"services\"", "\"edges\"", "\"anomalies\"", "\"cleanup_recommendations\""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.report)
			if err != nil {
				t.Fatalf("failed to marshal report: %v", err)
			}
			encoded := string(payload)
			for _, key := range tc.keys {
				if !strings.Contains(encoded, key) {
					t.Fatalf("expected JSON to contain %s, got %s", key, encoded)
				}
			}
		})
	}
}

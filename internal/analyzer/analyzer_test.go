package analyzer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

type fakeCollector struct {
	tables map[string]*models.Table
	err    error
}

func (f *fakeCollector) FetchTableMetadata(ctx context.Context) (map[string]*models.Table, error) {
	return f.tables, f.err
}

func TestAnalyzeBuildsModels(t *testing.T) {
	entries := loadFixtureEntries(t, "query_logs.json")
	cfg := config.DefaultConfig()
	cfg.ResolveK8s = false
	cfg.DetectUnusedTables = false
	cfg.AnomalyDetection = true
	cfg.Verbose = false

	analyzer := New(cfg, nil, &fakeCollector{})
	if err := analyzer.Analyze(context.Background(), entries); err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	tables := analyzer.Tables()
	if len(tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(tables))
	}

	table1, ok := tables["db1.table1"]
	if !ok {
		t.Fatalf("expected db1.table1 in tables")
	}
	if table1.Reads != 10 {
		t.Fatalf("expected db1.table1 reads 10, got %d", table1.Reads)
	}
	if table1.Writes != 3 {
		t.Fatalf("expected db1.table1 writes 3, got %d", table1.Writes)
	}

	table2, ok := tables["db2.table2"]
	if !ok {
		t.Fatalf("expected db2.table2 in tables")
	}
	if table2.Reads != 5 {
		t.Fatalf("expected db2.table2 reads 5, got %d", table2.Reads)
	}

	services := analyzer.Services()
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	svc1, ok := services["10.0.0.1"]
	if !ok {
		t.Fatalf("expected service 10.0.0.1")
	}
	if svc1.QueryCount != 2 {
		t.Fatalf("expected service 10.0.0.1 query count 2, got %d", svc1.QueryCount)
	}
	if !containsString(svc1.TablesUsed, "db1.table1") {
		t.Fatalf("expected service 10.0.0.1 to use db1.table1")
	}

	svc2, ok := services["10.0.0.2"]
	if !ok {
		t.Fatalf("expected service 10.0.0.2")
	}
	if svc2.QueryCount != 1 {
		t.Fatalf("expected service 10.0.0.2 query count 1, got %d", svc2.QueryCount)
	}
	if !containsString(svc2.TablesUsed, "db1.table1") || !containsString(svc2.TablesUsed, "db2.table2") {
		t.Fatalf("expected service 10.0.0.2 to use db1.table1 and db2.table2")
	}

	edges := analyzer.Edges()
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(edges))
	}

	anomalies := analyzer.Anomalies()
	table2Anomalies := map[string]bool{}
	for _, anomaly := range anomalies {
		if anomaly.AffectedTable == "db2.table2" {
			table2Anomalies[anomaly.Type] = true
		}
	}
	if !table2Anomalies["stale_table"] {
		t.Fatalf("expected stale_table anomaly for db2.table2")
	}
	if !table2Anomalies["low_activity"] {
		t.Fatalf("expected low_activity anomaly for db2.table2")
	}
}

func TestAnalyzeEnrichesInventory(t *testing.T) {
	entries := loadFixtureEntries(t, "query_logs.json")
	cfg := config.DefaultConfig()
	cfg.ResolveK8s = false
	cfg.DetectUnusedTables = true
	cfg.AnomalyDetection = false
	cfg.Verbose = false

	metaTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	collector := &fakeCollector{
		tables: map[string]*models.Table{
			"db1.table1": {
				Name:         "table1",
				Database:     "db1",
				FullName:     "db1.table1",
				Engine:       "ReplicatedMergeTree",
				IsReplicated: true,
				TotalBytes:   123,
				TotalRows:    45,
				CreateTime:   metaTime,
				IsMV:         false,
				MVDependency: []string{"db1.dep"},
			},
			"db3.table3": {
				Name:       "table3",
				Database:   "db3",
				FullName:   "db3.table3",
				Engine:     "MergeTree",
				TotalBytes: 10,
				TotalRows:  1,
				CreateTime: metaTime,
			},
		},
	}

	analyzer := New(cfg, nil, collector)
	if err := analyzer.Analyze(context.Background(), entries); err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	tables := analyzer.Tables()
	table1, ok := tables["db1.table1"]
	if !ok {
		t.Fatalf("expected db1.table1 in tables")
	}
	if table1.Engine != "ReplicatedMergeTree" || !table1.IsReplicated {
		t.Fatalf("expected db1.table1 metadata to be enriched")
	}

	table3, ok := tables["db3.table3"]
	if !ok {
		t.Fatalf("expected db3.table3 in tables")
	}
	if !table3.ZeroUsage {
		t.Fatalf("expected db3.table3 to be marked zero usage")
	}
	if table3.Reads != 0 || table3.Writes != 0 {
		t.Fatalf("expected db3.table3 reads/writes to be zero")
	}
	if !table3.LastAccess.IsZero() || !table3.FirstSeen.IsZero() {
		t.Fatalf("expected db3.table3 access times to be zero")
	}
}

func TestAnalyzeRespectsExclusions(t *testing.T) {
	entries := []*models.QueryLogEntry{
		{
			QueryID:   "q1",
			EventTime: time.Date(2026, 2, 16, 12, 0, 0, 0, time.UTC),
			QueryKind: "SELECT",
			ClientIP:  "10.0.0.1",
			ReadRows:  10,
			Tables: []string{
				"analytics.events",
				"analytics.tmp_stage",
				"tmpdb.sessions",
			},
		},
	}

	cfg := config.DefaultConfig()
	cfg.AnomalyDetection = false
	cfg.ResolveK8s = false
	cfg.ExcludeTables = []string{"analytics.tmp_*"}
	cfg.ExcludeDatabases = []string{"tmp*"}
	cfg.Normalize()

	analyzer := New(cfg, nil, &fakeCollector{})
	if err := analyzer.Analyze(context.Background(), entries); err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(analyzer.Tables()) != 1 {
		t.Fatalf("expected only one table after exclusions, got %d", len(analyzer.Tables()))
	}
	if _, found := analyzer.Tables()["analytics.events"]; !found {
		t.Fatalf("expected analytics.events to remain after exclusions")
	}
	if _, found := analyzer.Tables()["analytics.tmp_stage"]; found {
		t.Fatalf("did not expect excluded table analytics.tmp_stage")
	}
	if _, found := analyzer.Tables()["tmpdb.sessions"]; found {
		t.Fatalf("did not expect excluded database table tmpdb.sessions")
	}

	svc := analyzer.Services()["10.0.0.1"]
	if svc == nil {
		t.Fatalf("expected service 10.0.0.1")
	}
	if len(svc.TablesUsed) != 1 || svc.TablesUsed[0] != "analytics.events" {
		t.Fatalf("expected only analytics.events in service tables, got %v", svc.TablesUsed)
	}

	if len(analyzer.Edges()) != 1 || analyzer.Edges()[0].TableName != "analytics.events" {
		t.Fatalf("expected one edge to analytics.events, got %+v", analyzer.Edges())
	}
}

func TestQueryKindHelpers(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		isRead  bool
		isWrite bool
	}{
		{name: "select", kind: "SELECT", isRead: true, isWrite: false},
		{name: "select_lower", kind: "select", isRead: true, isWrite: false},
		{name: "insert", kind: "INSERT", isRead: false, isWrite: true},
		{name: "create", kind: "CREATE", isRead: false, isWrite: true},
		{name: "drop", kind: "DROP", isRead: false, isWrite: true},
		{name: "unknown", kind: "SHOW", isRead: false, isWrite: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isReadQuery(tc.kind); got != tc.isRead {
				t.Fatalf("expected isReadQuery=%v, got %v", tc.isRead, got)
			}
			if got := isWriteQuery(tc.kind); got != tc.isWrite {
				t.Fatalf("expected isWriteQuery=%v, got %v", tc.isWrite, got)
			}
		})
	}
}

func loadFixtureEntries(t *testing.T, filename string) []*models.QueryLogEntry {
	t.Helper()
	path := filepath.Join("testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", path, err)
	}

	var entries []*models.QueryLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("failed to unmarshal fixture %s: %v", path, err)
	}

	return entries
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

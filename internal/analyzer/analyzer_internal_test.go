package analyzer

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/k8s"
	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// Mock K8s Resolver
type mockK8sResolver struct {
	resolveIPFunc func(ctx context.Context, ip string) (*k8s.ServiceInfo, error)
}

func (m *mockK8sResolver) ResolveIP(ctx context.Context, ip string) (*k8s.ServiceInfo, error) {
	if m.resolveIPFunc != nil {
		return m.resolveIPFunc(ctx, ip)
	}
	return nil, nil
}

func (m *mockK8sResolver) Close() error {
	return nil
}

func TestBuildTableModel(t *testing.T) {
	now := time.Now().Truncate(time.Second) // Truncate for consistent comparison
	cfg := config.DefaultConfig()
	cfg.ExcludeTables = []string{"db1.excluded_table"}
	cfg.Normalize()

	a := New(cfg, nil, nil) // Resolver and collector not needed for this test

	entries := []*models.QueryLogEntry{
		{
			QueryID:   "q1",
			EventTime: now.Add(-2 * time.Hour),
			QueryKind: "SELECT",
			ReadRows:  10,
			Tables:    []string{"db1.table1", "db2.table2"},
		},
		{
			QueryID:     "q2",
			EventTime:   now.Add(-1 * time.Hour),
			QueryKind:   "INSERT",
			WrittenRows: 5,
			Tables:      []string{"db1.table1"},
		},
		{
			QueryID:   "q3",
			EventTime: now.Add(-3 * time.Hour),
			QueryKind: "SELECT",
			ReadRows:  20,
			Tables:    []string{"db2.table2"},
		},
		{
			QueryID:   "q4",
			EventTime: now.Add(-30 * time.Minute),
			QueryKind: "SELECT",
			ReadRows:  1,
			Tables:    []string{"db1.excluded_table"}, // Should be excluded
		},
		{
			QueryID:   "q5",
			EventTime: now.Add(-10 * time.Minute),
			QueryKind: "SELECT",
			ReadRows:  100,
			Tables:    []string{"table_no_db"}, // Table without database
		},
		{
			QueryID:   "q6",
			EventTime: now.Add(-5 * time.Minute),
			QueryKind: "SELECT",
			ReadRows:  0, // No rows read
			Tables:    []string{"db1.table1"},
		},
		{
			QueryID:     "q7",
			EventTime:   now.Add(-1 * time.Minute),
			QueryKind:   "ALTER",
			WrittenRows: 1, // Write operation, 1 row written
			Tables:      []string{"db1.table1"},
		},
	}

	if err := a.buildTableModel(entries); err != nil { // Directly call the internal method
		t.Fatalf("buildTableModel failed: %v", err)
	}

	tables := a.Tables()

	// Verify db1.table1
	table1, ok := tables["db1.table1"]
	if !ok {
		t.Fatalf("expected db1.table1")
	}
	if table1.Reads != 10 { // Only from q1
		t.Errorf("db1.table1 reads expected 10, got %d", table1.Reads)
	}
	if table1.Writes != 6 { // 5 from q2 + 1 from q7
		t.Errorf("db1.table1 writes expected 6, got %d", table1.Writes)
	}
	if !table1.FirstSeen.Equal(now.Add(-2 * time.Hour)) { // First seen from q1
		t.Errorf("db1.table1 first seen expected %v, got %v", now.Add(-2*time.Hour), table1.FirstSeen)
	}
	if !table1.LastAccess.Equal(now.Add(-1 * time.Minute)) { // Last access from q7
		t.Errorf("db1.table1 last access expected %v, got %v", now.Add(-1*time.Minute), table1.LastAccess)
	}

	// Verify db2.table2
	table2, ok := tables["db2.table2"]
	if !ok {
		t.Fatalf("expected db2.table2")
	}
	if table2.Reads != 30 { // 10 from q1 + 20 from q3
		t.Errorf("db2.table2 reads expected 30, got %d", table2.Reads)
	}
	if table2.Writes != 0 {
		t.Errorf("db2.table2 writes expected 0, got %d", table2.Writes)
	}
	if !table2.FirstSeen.Equal(now.Add(-3 * time.Hour)) { // First seen from q3
		t.Errorf("db2.table2 first seen expected %v, got %v", now.Add(-3*time.Hour), table2.FirstSeen)
	}
	if !table2.LastAccess.Equal(now.Add(-2 * time.Hour)) { // Last access from q1
		t.Errorf("db2.table2 last access expected %v, got %v", now.Add(-2*time.Hour), table2.LastAccess)
	}

	// Verify table_no_db
	tableNoDB, ok := tables["table_no_db"]
	if !ok {
		t.Fatalf("expected table_no_db")
	}
	if tableNoDB.Reads != 100 {
		t.Errorf("table_no_db reads expected 100, got %d", tableNoDB.Reads)
	}
	if tableNoDB.Database != "" {
		t.Errorf("table_no_db database expected empty, got %s", tableNoDB.Database)
	}
	if tableNoDB.Name != "table_no_db" {
		t.Errorf("table_no_db name expected 'table_no_db', got %s", tableNoDB.Name)
	}

	// Verify excluded_table is not present
	if _, ok := tables["db1.excluded_table"]; ok {
		t.Fatalf("excluded table db1.excluded_table should not be present")
	}

	// Test with empty entries
	a = New(cfg, nil, nil)
	if err := a.buildTableModel([]*models.QueryLogEntry{}); err != nil {
		t.Fatalf("buildTableModel with empty entries failed: %v", err)
	}
	if len(a.Tables()) != 0 {
		t.Errorf("expected 0 tables for empty entries, got %d", len(a.Tables()))
	}

	// Test with entries containing empty table names
	a = New(cfg, nil, nil)
	emptyTableNameEntries := []*models.QueryLogEntry{
		{
			QueryID:   "q8",
			EventTime: now,
			QueryKind: "SELECT",
			Tables:    []string{"", "db.valid_table"},
		},
	}
	if err := a.buildTableModel(emptyTableNameEntries); err != nil {
		t.Fatalf("buildTableModel with empty table name failed: %v", err)
	}
	if _, ok := a.Tables()["db.valid_table"]; !ok {
		t.Errorf("expected db.valid_table")
	}
	if len(a.Tables()) != 1 {
		t.Errorf("expected 1 table, got %d", len(a.Tables()))
	}
}

func TestGenerateSparklines(t *testing.T) {
	now := time.Now().Truncate(time.Hour) // Use truncated time for consistent hourly buckets
	cfg := config.DefaultConfig()

	// Test case 1: Single entry for a table
	a1 := New(cfg, nil, nil)
	entries1 := []*models.QueryLogEntry{
		{EventTime: now.Add(10 * time.Minute), Tables: []string{"db.table1"}},
	}
	// Manually populate tables map for sparkline generation
	a1.Tables()["db.table1"] = &models.Table{FullName: "db.table1"}

	if err := a1.generateSparklines(entries1); err != nil {
		t.Fatalf("generateSparklines failed: %v", err)
	}
	expectedSparkline1 := []models.TimeSeriesPoint{
		{Timestamp: now, Value: 1},
	}
	if !reflect.DeepEqual(a1.Tables()["db.table1"].Sparkline, expectedSparkline1) {
		t.Errorf("expected sparkline %v, got %v", expectedSparkline1, a1.Tables()["db.table1"].Sparkline)
	}

	// Test case 2: Multiple entries for a table within the same hour bucket
	a2 := New(cfg, nil, nil)
	entries2 := []*models.QueryLogEntry{
		{EventTime: now.Add(10 * time.Minute), Tables: []string{"db.table2"}},
		{EventTime: now.Add(20 * time.Minute), Tables: []string{"db.table2"}},
		{EventTime: now.Add(30 * time.Minute), Tables: []string{"db.table2"}},
	}
	a2.Tables()["db.table2"] = &models.Table{FullName: "db.table2"}

	if err := a2.generateSparklines(entries2); err != nil {
		t.Fatalf("generateSparklines failed: %v", err)
	}
	expectedSparkline2 := []models.TimeSeriesPoint{
		{Timestamp: now, Value: 3},
	}
	if !reflect.DeepEqual(a2.Tables()["db.table2"].Sparkline, expectedSparkline2) {
		t.Errorf("expected sparkline %v, got %v", expectedSparkline2, a2.Tables()["db.table2"].Sparkline)
	}

	// Test case 3: Multiple entries for a table across different hour buckets
	a3 := New(cfg, nil, nil)
	entries3 := []*models.QueryLogEntry{
		{EventTime: now.Add(-1 * time.Hour).Add(10 * time.Minute), Tables: []string{"db.table3"}},
		{EventTime: now.Add(-1 * time.Hour).Add(20 * time.Minute), Tables: []string{"db.table3"}},
		{EventTime: now.Add(30 * time.Minute), Tables: []string{"db.table3"}},
	}
	a3.Tables()["db.table3"] = &models.Table{FullName: "db.table3"}

	if err := a3.generateSparklines(entries3); err != nil {
		t.Fatalf("generateSparklines failed: %v", err)
	}
	expectedSparkline3 := []models.TimeSeriesPoint{
		{Timestamp: now.Add(-1 * time.Hour), Value: 2},
		{Timestamp: now, Value: 1},
	}
	if !reflect.DeepEqual(a3.Tables()["db.table3"].Sparkline, expectedSparkline3) {
		t.Errorf("expected sparkline %v, got %v", expectedSparkline3, a3.Tables()["db.table3"].Sparkline)
	}

	// Test case 4: Multiple tables
	a4 := New(cfg, nil, nil)
	entries4 := []*models.QueryLogEntry{
		{EventTime: now.Add(-1 * time.Hour), Tables: []string{"db.tableA"}},
		{EventTime: now.Add(-1 * time.Hour), Tables: []string{"db.tableB"}},
		{EventTime: now, Tables: []string{"db.tableA"}},
	}
	a4.Tables()["db.tableA"] = &models.Table{FullName: "db.tableA"}
	a4.Tables()["db.tableB"] = &models.Table{FullName: "db.tableB"}

	if err := a4.generateSparklines(entries4); err != nil {
		t.Fatalf("generateSparklines failed: %v", err)
	}
	expectedSparklineA := []models.TimeSeriesPoint{
		{Timestamp: now.Add(-1 * time.Hour), Value: 1},
		{Timestamp: now, Value: 1},
	}
	expectedSparklineB := []models.TimeSeriesPoint{
		{Timestamp: now.Add(-1 * time.Hour), Value: 1},
	}
	if !reflect.DeepEqual(a4.Tables()["db.tableA"].Sparkline, expectedSparklineA) {
		t.Errorf("expected sparkline A %v, got %v", expectedSparklineA, a4.Tables()["db.tableA"].Sparkline)
	}
	if !reflect.DeepEqual(a4.Tables()["db.tableB"].Sparkline, expectedSparklineB) {
		t.Errorf("expected sparkline B %v, got %v", expectedSparklineB, a4.Tables()["db.tableB"].Sparkline)
	}

	// Test case 5: Empty entries list
	a5 := New(cfg, nil, nil)
	if err := a5.generateSparklines([]*models.QueryLogEntry{}); err != nil {
		t.Fatalf("generateSparklines with empty entries failed: %v", err)
	}
	// No tables, so no sparklines will be generated
}

func TestBuildServiceModel(t *testing.T) {
	now := time.Now().Truncate(time.Second) // Truncate for consistent comparison
	cfg := config.DefaultConfig()
	cfg.ExcludeTables = []string{"db1.excluded_table"}
	cfg.Normalize()

	// Mock resolver for K8s resolution tests
	mockResolver := &mockK8sResolver{}

	// Test case 1: Basic service creation and updates without K8s resolution
	a1 := New(cfg, nil, nil) // No resolver
	entries1 := []*models.QueryLogEntry{
		{EventTime: now.Add(-2 * time.Hour), ClientIP: "1.1.1.1", Tables: []string{"db.t1"}},
		{EventTime: now.Add(-1 * time.Hour), ClientIP: "1.1.1.1", Tables: []string{"db.t1", "db.t2"}},
		{EventTime: now.Add(-3 * time.Hour), ClientIP: "2.2.2.2", Tables: []string{"db.t3"}},
		{EventTime: now.Add(-30 * time.Minute), ClientIP: "1.1.1.1", Tables: []string{"db.t1", "db1.excluded_table"}},
	}

	if err := a1.buildServiceModel(context.Background(), entries1); err != nil {
		t.Fatalf("buildServiceModel failed: %v", err)
	}

	services1 := a1.Services()
	if len(services1) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services1))
	}

	svc1_1, ok := services1["1.1.1.1"]
	if !ok {
		t.Fatalf("expected service 1.1.1.1")
	}
	if svc1_1.QueryCount != 3 {
		t.Errorf("1.1.1.1 query count expected 3, got %d", svc1_1.QueryCount)
	}
	if !svc1_1.LastSeen.Equal(now.Add(-30 * time.Minute)) {
		t.Errorf("1.1.1.1 last seen expected %v, got %v", now.Add(-30*time.Minute), svc1_1.LastSeen)
	}
	expectedTables1_1 := []string{"db.t1", "db.t2"}
	if !reflect.DeepEqual(svc1_1.TablesUsed, expectedTables1_1) {
		t.Errorf("1.1.1.1 tables used expected %v, got %v", expectedTables1_1, svc1_1.TablesUsed)
	}

	// Test case 2: K8s resolution enabled, resolver returns info
	a2 := New(cfg, mockResolver, nil)
	cfg.ResolveK8s = true
	mockResolver.resolveIPFunc = func(ctx context.Context, ip string) (*k8s.ServiceInfo, error) {
		if ip == "3.3.3.3" {
			return &k8s.ServiceInfo{Service: "k8s-svc", Namespace: "default", Pod: "k8s-pod"}, nil
		}
		return nil, nil
	}
	entries2 := []*models.QueryLogEntry{
		{EventTime: now, ClientIP: "3.3.3.3", Tables: []string{"db.t4"}},
	}

	if err := a2.buildServiceModel(context.Background(), entries2); err != nil {
		t.Fatalf("buildServiceModel K8s failed: %v", err)
	}

	services2 := a2.Services()
	svc2_1, ok := services2["3.3.3.3"]
	if !ok {
		t.Fatalf("expected service 3.3.3.3")
	}
	if svc2_1.K8sService != "k8s-svc" || svc2_1.K8sNamespace != "default" || svc2_1.K8sPod != "k8s-pod" {
		t.Errorf("expected K8s info, got %+v", svc2_1)
	}

	// Test case 3: K8s resolution enabled, resolver returns nil info
	a3 := New(cfg, mockResolver, nil)
	mockResolver.resolveIPFunc = func(ctx context.Context, ip string) (*k8s.ServiceInfo, error) {
		return nil, nil // Return nil info
	}
	entries3 := []*models.QueryLogEntry{
		{EventTime: now, ClientIP: "4.4.4.4", Tables: []string{"db.t5"}},
	}
	if err := a3.buildServiceModel(context.Background(), entries3); err != nil {
		t.Fatalf("buildServiceModel K8s nil info failed: %v", err)
	}
	services3 := a3.Services()
	svc3_1, ok := services3["4.4.4.4"]
	if !ok {
		t.Fatalf("expected service 4.4.4.4")
	}
	if svc3_1.K8sService != "" || svc3_1.K8sNamespace != "" || svc3_1.K8sPod != "" {
		t.Errorf("expected no K8s info, got %+v", svc3_1)
	}

	// Test case 4: K8s resolution enabled, resolver returns error
	a4 := New(cfg, mockResolver, nil)
	mockResolver.resolveIPFunc = func(ctx context.Context, ip string) (*k8s.ServiceInfo, error) {
		return nil, k8s.ErrNotFound // Simulate an error
	}
	entries4 := []*models.QueryLogEntry{
		{EventTime: now, ClientIP: "5.5.5.5", Tables: []string{"db.t6"}},
	}
	if err := a4.buildServiceModel(context.Background(), entries4); err != nil {
		t.Fatalf("buildServiceModel K8s error failed: %v", err)
	}
	services4 := a4.Services()
	svc4_1, ok := services4["5.5.5.5"]
	if !ok {
		t.Fatalf("expected service 5.5.5.5")
	}
	if svc4_1.K8sService != "" || svc4_1.K8sNamespace != "" || svc4_1.K8sPod != "" {
		t.Errorf("expected no K8s info on error, got %+v", svc4_1)
	}

	// Test case 5: Empty entries list
	a5 := New(cfg, nil, nil)
	if err := a5.buildServiceModel(context.Background(), []*models.QueryLogEntry{}); err != nil {
		t.Fatalf("buildServiceModel with empty entries failed: %v", err)
	}
	if len(a5.Services()) != 0 {
		t.Errorf("expected 0 services for empty entries, got %d", len(a5.Services()))
	}

	// Test case 6: Entries with empty client IP
	a6 := New(cfg, nil, nil)
	emptyClientIPEntries := []*models.QueryLogEntry{
		{EventTime: now, ClientIP: "", Tables: []string{"db.t7"}},
		{EventTime: now, ClientIP: "6.6.6.6", Tables: []string{"db.t7"}},
	}
	if err := a6.buildServiceModel(context.Background(), emptyClientIPEntries); err != nil {
		t.Fatalf("buildServiceModel with empty client IP failed: %v", err)
	}
	if _, ok := a6.Services()["6.6.6.6"]; !ok {
		t.Errorf("expected service 6.6.6.6")
	}
	if len(a6.Services()) != 1 {
		t.Errorf("expected 1 service, got %d", len(a6.Services()))
	}
}

func TestBuildEdges(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	cfg := config.DefaultConfig()
	cfg.ExcludeTables = []string{"db1.excluded_table"}
	cfg.Normalize()

	a := New(cfg, nil, nil)

	// Manually populate services map for ServiceName resolution
	a.Services()["1.1.1.1"] = &models.Service{IP: "1.1.1.1", K8sService: "app-svc"}
	a.Services()["2.2.2.2"] = &models.Service{IP: "2.2.2.2"} // No K8s service name

	entries := []*models.QueryLogEntry{
		{
			QueryID:   "q1",
			EventTime: now.Add(-3 * time.Hour),
			ClientIP:  "1.1.1.1",
			QueryKind: "SELECT",
			ReadRows:  10,
			Tables:    []string{"db.tableA", "db.tableB"},
		},
		{
			QueryID:     "q2",
			EventTime:   now.Add(-2 * time.Hour),
			ClientIP:    "1.1.1.1",
			QueryKind:   "INSERT",
			WrittenRows: 5,
			Tables:      []string{"db.tableA"}, // Update existing edge
		},
		{
			QueryID:   "q3",
			EventTime: now.Add(-1 * time.Hour),
			ClientIP:  "2.2.2.2",
			QueryKind: "SELECT",
			ReadRows:  20,
			Tables:    []string{"db.tableC"},
		},
		{
			QueryID:   "q4",
			EventTime: now.Add(-30 * time.Minute),
			ClientIP:  "1.1.1.1",
			QueryKind: "SELECT",
			ReadRows:  1,
			Tables:    []string{"db1.excluded_table"}, // Excluded table
		},
		{
			QueryID:     "q5",
			EventTime:   now.Add(-10 * time.Minute),
			ClientIP:    "1.1.1.1",
			QueryKind:   "ALTER",
			WrittenRows: 2,
			Tables:      []string{"db.tableB"}, // Update existing edge
		},
	}

	if err := a.buildEdges(entries); err != nil {
		t.Fatalf("buildEdges failed: %v", err)
	}

	edges := a.Edges()
	if len(edges) != 3 {
		t.Fatalf("expected 3 edges, got %d", len(edges))
	}

	// Verify edge 1.1.1.1 -> db.tableA
	edge1A := findEdge(edges, "1.1.1.1", "db.tableA")
	if edge1A == nil {
		t.Fatalf("expected edge 1.1.1.1 -> db.tableA")
	}
	if edge1A.Reads != 10 {
		t.Errorf("edge 1.1.1.1 -> db.tableA reads expected 10, got %d", edge1A.Reads)
	}
	if edge1A.Writes != 5 {
		t.Errorf("edge 1.1.1.1 -> db.tableA writes expected 5, got %d", edge1A.Writes)
	}
	if !edge1A.LastActivity.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("edge 1.1.1.1 -> db.tableA last activity expected %v, got %v", now.Add(-2*time.Hour), edge1A.LastActivity)
	}
	if edge1A.ServiceName != "app-svc" {
		t.Errorf("edge 1.1.1.1 -> db.tableA service name expected 'app-svc', got %s", edge1A.ServiceName)
	}

	// Verify edge 1.1.1.1 -> db.tableB
	edge1B := findEdge(edges, "1.1.1.1", "db.tableB")
	if edge1B == nil {
		t.Fatalf("expected edge 1.1.1.1 -> db.tableB")
	}
	if edge1B.Reads != 10 {
		t.Errorf("edge 1.1.1.1 -> db.tableB reads expected 10, got %d", edge1B.Reads)
	}
	if edge1B.Writes != 2 {
		t.Errorf("edge 1.1.1.1 -> db.tableB writes expected 2, got %d", edge1B.Writes)
	}
	if !edge1B.LastActivity.Equal(now.Add(-10 * time.Minute)) {
		t.Errorf("edge 1.1.1.1 -> db.tableB last activity expected %v, got %v", now.Add(-10*time.Minute), edge1B.LastActivity)
	}

	// Verify edge 2.2.2.2 -> db.tableC
	edge2C := findEdge(edges, "2.2.2.2", "db.tableC")
	if edge2C == nil {
		t.Fatalf("expected edge 2.2.2.2 -> db.tableC")
	}
	if edge2C.Reads != 20 {
		t.Errorf("edge 2.2.2.2 -> db.tableC reads expected 20, got %d", edge2C.Reads)
	}
	if edge2C.Writes != 0 {
		t.Errorf("edge 2.2.2.2 -> db.tableC writes expected 0, got %d", edge2C.Writes)
	}
	if !edge2C.LastActivity.Equal(now.Add(-1 * time.Hour)) {
		t.Errorf("edge 2.2.2.2 -> db.tableC last activity expected %v, got %v", now.Add(-1*time.Hour), edge2C.LastActivity)
	}
	if edge2C.ServiceName != "2.2.2.2" { // Should default to IP
		t.Errorf("edge 2.2.2.2 -> db.tableC service name expected '2.2.2.2', got %s", edge2C.ServiceName)
	}

	// Verify excluded table does not create an edge
	if findEdge(edges, "1.1.1.1", "db1.excluded_table") != nil {
		t.Fatalf("excluded table db1.excluded_table should not have an edge")
	}

	// Test with empty entries
	aEmpty := New(cfg, nil, nil)
	if err := aEmpty.buildEdges([]*models.QueryLogEntry{}); err != nil {
		t.Fatalf("buildEdges with empty entries failed: %v", err)
	}
	if len(aEmpty.Edges()) != 0 {
		t.Errorf("expected 0 edges for empty entries, got %d", len(aEmpty.Edges()))
	}

	// Test with entries containing empty client IP
	aEmptyIP := New(cfg, nil, nil)
	emptyClientIPEntries := []*models.QueryLogEntry{
		{EventTime: now, ClientIP: "", Tables: []string{"db.t_ip"}},
		{EventTime: now, ClientIP: "3.3.3.3", Tables: []string{"db.t_ip"}},
	}
	aEmptyIP.Services()["3.3.3.3"] = &models.Service{IP: "3.3.3.3"}
	if err := aEmptyIP.buildEdges(emptyClientIPEntries); err != nil {
		t.Fatalf("buildEdges with empty client IP failed: %v", err)
	}
	if findEdge(aEmptyIP.Edges(), "", "db.t_ip") != nil {
		t.Errorf("expected no edge for empty client IP")
	}
	if findEdge(aEmptyIP.Edges(), "3.3.3.3", "db.t_ip") == nil {
		t.Errorf("expected edge for valid client IP")
	}
	if len(aEmptyIP.Edges()) != 1 {
		t.Errorf("expected 1 edge, got %d", len(aEmptyIP.Edges()))
	}
}

func findEdge(edges []*models.Edge, serviceIP, tableName string) *models.Edge {
	for _, edge := range edges {
		if edge.ServiceIP == serviceIP && edge.TableName == tableName {
			return edge
		}
	}
	return nil
}

func TestDetectAnomalies(t *testing.T) {
	now := time.Now()
	cfg := config.DefaultConfig()

	// Helper to create an analyzer with specific tables and services
	newTestAnalyzer := func(tables map[string]*models.Table, services map[string]*models.Service) *Analyzer {
		a := New(cfg, nil, nil)
		for k, v := range tables {
			a.Tables()[k] = v
		}
		for k, v := range services {
			a.Services()[k] = v
		}
		return a
	}

	// Test Case: single_access anomaly
	t.Run("single_access", func(t *testing.T) {
		tables := map[string]*models.Table{
			"db.single_access_table": {Reads: 1, Writes: 0, LastAccess: now.Add(-time.Hour)},
		}
		a := newTestAnalyzer(tables, nil)
		if err := a.detectAnomalies(); err != nil {
			t.Fatalf("detectAnomalies failed: %v", err)
		}
		anomalies := a.Anomalies()
		if len(anomalies) != 1 {
			t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
		}
		if anomalies[0].Type != "single_access" {
			t.Errorf("expected anomaly type single_access, got %s", anomalies[0].Type)
		}
	})

	// Test Case: stale_table anomaly
	t.Run("stale_table", func(t *testing.T) {
		tables := map[string]*models.Table{
			"db.stale_table": {Reads: 10, Writes: 1, LastAccess: now.Add(-31 * 24 * time.Hour)}, // > 30 days
		}
		a := newTestAnalyzer(tables, nil)
		if err := a.detectAnomalies(); err != nil {
			t.Fatalf("detectAnomalies failed: %v", err)
		}
		anomalies := a.Anomalies()
		if len(anomalies) != 1 {
			t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
		}
		if anomalies[0].Type != "stale_table" {
			t.Errorf("expected anomaly type stale_table, got %s", anomalies[0].Type)
		}
	})

	// Test Case: write_only anomaly
	t.Run("write_only", func(t *testing.T) {
		tables := map[string]*models.Table{
			"db.write_only_table": {Reads: 0, Writes: 5, LastAccess: now.Add(-time.Hour)},
		}
		a := newTestAnalyzer(tables, nil)
		if err := a.detectAnomalies(); err != nil {
			t.Fatalf("detectAnomalies failed: %v", err)
		}
		anomalies := a.Anomalies()
		if len(anomalies) != 1 {
			t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
		}
		if anomalies[0].Type != "write_only" {
			t.Errorf("expected anomaly type write_only, got %s", anomalies[0].Type)
		}
	})

	// Test Case: read_only anomaly
	t.Run("read_only", func(t *testing.T) {
		tables := map[string]*models.Table{
			"db.read_only_table": {Reads: 101, Writes: 0, LastAccess: now.Add(-time.Hour)}, // Reads > 100
		}
		a := newTestAnalyzer(tables, nil)
		if err := a.detectAnomalies(); err != nil {
			t.Fatalf("detectAnomalies failed: %v", err)
		}
		anomalies := a.Anomalies()
		if len(anomalies) != 1 {
			t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
		}
		if anomalies[0].Type != "read_only" {
			t.Errorf("expected anomaly type read_only, got %s", anomalies[0].Type)
		}
	})

	// Test Case: low_activity anomaly
	t.Run("low_activity", func(t *testing.T) {
		tables := map[string]*models.Table{
			"db.low_activity_table": {Reads: 5, Writes: 2, LastAccess: now.Add(-8 * 24 * time.Hour)}, // totalAccess < 10, daysSinceAccess > 7
		}
		a := newTestAnalyzer(tables, nil)
		if err := a.detectAnomalies(); err != nil {
			t.Fatalf("detectAnomalies failed: %v", err)
		}
		anomalies := a.Anomalies()
		if len(anomalies) != 1 {
			t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
		}
		if anomalies[0].Type != "low_activity" {
			t.Errorf("expected anomaly type low_activity, got %s", anomalies[0].Type)
		}
	})

	// Test Case: broad_access anomaly
	t.Run("broad_access", func(t *testing.T) {
		tablesUsed := make([]string, 21)
		for i := 0; i < 21; i++ {
			tablesUsed[i] = "db.table" + (string)(rune('A'+i))
		}
		services := map[string]*models.Service{
			"10.0.0.1": {TablesUsed: tablesUsed}, // > 20 tables
		}
		a := newTestAnalyzer(nil, services)
		if err := a.detectAnomalies(); err != nil {
			t.Fatalf("detectAnomalies failed: %v", err)
		}
		anomalies := a.Anomalies()
		if len(anomalies) != 1 {
			t.Fatalf("expected 1 anomaly, got %d", len(anomalies))
		}
		if anomalies[0].Type != "broad_access" {
			t.Errorf("expected anomaly type broad_access, got %s", anomalies[0].Type)
		}
	})

	// Test Case: No anomalies
	t.Run("no_anomalies", func(t *testing.T) {
		tables := map[string]*models.Table{
			"db.active_table": {Reads: 100, Writes: 10, LastAccess: now.Add(-time.Hour)},
		}
		services := map[string]*models.Service{
			"10.0.0.1": {TablesUsed: []string{"db.active_table"}},
		}
		a := newTestAnalyzer(tables, services)
		if err := a.detectAnomalies(); err != nil {
			t.Fatalf("detectAnomalies failed: %v", err)
		}
		anomalies := a.Anomalies()
		if len(anomalies) != 0 {
			t.Fatalf("expected 0 anomalies, got %d", len(anomalies))
		}
	})
}

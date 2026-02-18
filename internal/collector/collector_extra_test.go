package collector

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func testQueryLogColumns() []string {
	return []string{
		"query_id",
		"type",
		"event_time",
		"query_kind",
		"query",
		"user",
		"client_ip",
		"read_rows",
		"written_rows",
		"query_duration_ms",
		"exception",
	}
}

func testQueryRow(id, query string, durationMs int64) []driver.Value {
	return []driver.Value{
		driver.Value(id),
		driver.Value("QueryFinish"),
		driver.Value(time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)),
		driver.Value("SELECT"),
		driver.Value(query),
		driver.Value("user"),
		driver.Value("10.0.0.1"),
		driver.Value(int64(10)),
		driver.Value(int64(0)),
		driver.Value(durationMs),
		driver.Value(""),
	}
}

func TestProcessBatchHandlesInvalidRowsAndTruncation(t *testing.T) {
	longQuery := "SELECT * FROM db.events " + strings.Repeat("x", 100100)

	state := &mockState{
		columns: testQueryLogColumns(),
		pages: [][][]driver.Value{
			{
				testQueryRow("good", "SELECT * FROM db.events", 150),
				// Wrong type for query_duration_ms to trigger scan error.
				{
					driver.Value("bad-scan"),
					driver.Value("QueryFinish"),
					driver.Value(time.Now()),
					driver.Value("SELECT"),
					driver.Value("SELECT 1"),
					driver.Value("user"),
					driver.Value("10.0.0.1"),
					driver.Value(int64(1)),
					driver.Value(int64(0)),
					driver.Value("not-an-int"),
					driver.Value(""),
				},
				// Empty query_id to trigger essential-field validation skip.
				testQueryRow("", "SELECT * FROM db.events", 10),
				testQueryRow("long", longQuery, 10),
			},
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	client := &ClickHouseClient{conn: db, config: config.DefaultConfig()}
	rows, err := db.QueryContext(context.Background(), "SELECT query log")
	if err != nil {
		t.Fatalf("failed to query mock rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	entries, err := client.processBatch(rows)
	if err != nil {
		t.Fatalf("processBatch failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 valid entries, got %d", len(entries))
	}
	if entries[0].QueryID != "good" {
		t.Fatalf("unexpected first entry id: %s", entries[0].QueryID)
	}
	if entries[0].Duration != 150*time.Millisecond {
		t.Fatalf("expected 150ms duration, got %s", entries[0].Duration)
	}
	if !strings.HasSuffix(entries[1].Query, "... [truncated]") {
		t.Fatalf("expected long query to be truncated, got length %d", len(entries[1].Query))
	}
}

func TestProcessBatchRowsErrorRecovery(t *testing.T) {
	state := &mockState{
		columns: testQueryLogColumns(),
		pages: [][][]driver.Value{
			{testQueryRow("good", "SELECT * FROM db.events", 10)},
		},
		rowsErr: map[int]error{
			0: errors.New("iteration boom"),
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	client := &ClickHouseClient{conn: db, config: config.DefaultConfig()}
	rows, err := db.QueryContext(context.Background(), "SELECT query log")
	if err != nil {
		t.Fatalf("failed to query mock rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	entries, err := client.processBatch(rows)
	if err != nil {
		t.Fatalf("expected recovery with nil error, got %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected recovered entry, got %d", len(entries))
	}
}

func TestProcessBatchAppliesExclusionFilters(t *testing.T) {
	state := &mockState{
		columns: testQueryLogColumns(),
		pages: [][][]driver.Value{
			{
				testQueryRow("q1", "SELECT * FROM db.keep JOIN db.tmp_stage ON 1=1 JOIN tmpdb.sessions ON 1=1", 20),
			},
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := config.DefaultConfig()
	cfg.ExcludeTables = []string{"db.tmp_*"}
	cfg.ExcludeDatabases = []string{"tmp*"}
	cfg.Normalize()

	client := &ClickHouseClient{conn: db, config: cfg}
	rows, err := db.QueryContext(context.Background(), "SELECT query log")
	if err != nil {
		t.Fatalf("failed to query mock rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	entries, err := client.processBatch(rows)
	if err != nil {
		t.Fatalf("processBatch failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	if len(entries[0].Tables) != 1 || entries[0].Tables[0] != "db.keep" {
		t.Fatalf("expected only db.keep after exclusions, got %v", entries[0].Tables)
	}
}

func TestProcessBatchRowsErrorWithoutEntries(t *testing.T) {
	state := &mockState{
		columns: testQueryLogColumns(),
		pages:   [][][]driver.Value{{}},
		rowsErr: map[int]error{
			0: errors.New("iteration boom"),
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	client := &ClickHouseClient{conn: db, config: config.DefaultConfig()}
	rows, err := db.QueryContext(context.Background(), "SELECT query log")
	if err != nil {
		t.Fatalf("failed to query mock rows: %v", err)
	}
	defer func() { _ = rows.Close() }()

	entries, err := client.processBatch(rows)
	if err == nil {
		t.Fatalf("expected iteration error, got entries=%v", entries)
	}
}

func TestCheckSchema(t *testing.T) {
	columns := []string{"name", "type", "default_type", "default_expression", "comment", "codec_expression", "ttl_expression"}

	successState := &mockState{
		columns: columns,
		pages: [][][]driver.Value{
			{
				{driver.Value("query_id"), driver.Value("String"), driver.Value(""), driver.Value(""), driver.Value(""), driver.Value(""), driver.Value("")},
				{driver.Value("event_time"), driver.Value("DateTime"), driver.Value(""), driver.Value(""), driver.Value(""), driver.Value(""), driver.Value("")},
			},
		},
	}

	successDB := newMockDB(t, successState)
	t.Cleanup(func() {
		_ = successDB.Close()
	})

	client := &ClickHouseClient{conn: successDB, config: config.DefaultConfig()}
	if err := client.CheckSchema(context.Background()); err != nil {
		t.Fatalf("expected schema check success, got %v", err)
	}

	errorState := &mockState{
		columns:  columns,
		queryErr: errors.New("describe failed"),
	}

	errorDB := newMockDB(t, errorState)
	t.Cleanup(func() {
		_ = errorDB.Close()
	})

	client = &ClickHouseClient{conn: errorDB, config: config.DefaultConfig()}
	err := client.CheckSchema(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to describe query_log") {
		t.Fatalf("expected describe failure, got %v", err)
	}
}

func TestFetchTableMetadata(t *testing.T) {
	columns := []string{
		"database",
		"name",
		"engine",
		"total_bytes",
		"total_rows",
		"create_time",
		"dep_databases",
		"dep_tables",
	}

	createTime := time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC)
	state := &mockState{
		columns: columns,
		pages: [][][]driver.Value{
			{
				{driver.Value("db1"), driver.Value("mv_table"), driver.Value("ReplicatedMergeTree"), driver.Value(int64(1024)), driver.Value(int64(10)), driver.Value(createTime), driver.Value("dbx,dby"), driver.Value("tx,ty")},
				{driver.Value("db2"), driver.Value("plain"), driver.Value("MergeTree"), nil, nil, driver.Value(createTime), nil, nil},
			},
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	client := &ClickHouseClient{conn: db, config: config.DefaultConfig()}
	tables, err := client.FetchTableMetadata(context.Background())
	if err != nil {
		t.Fatalf("FetchTableMetadata failed: %v", err)
	}
	if len(tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(tables))
	}

	mv := tables["db1.mv_table"]
	if mv == nil {
		t.Fatal("expected db1.mv_table metadata")
	}
	if !mv.IsReplicated {
		t.Fatal("expected replicated engine to be detected")
	}
	if mv.TotalBytes != 1024 || mv.TotalRows != 10 {
		t.Fatalf("unexpected totals: bytes=%d rows=%d", mv.TotalBytes, mv.TotalRows)
	}
	if len(mv.MVDependency) != 2 {
		t.Fatalf("expected parsed dependencies, got %v", mv.MVDependency)
	}
	if mv.Sparkline == nil {
		t.Fatal("expected sparkline slice to be initialized")
	}

	plain := tables["db2.plain"]
	if plain == nil {
		t.Fatal("expected db2.plain metadata")
	}
	if plain.TotalBytes != 0 || plain.TotalRows != 0 {
		t.Fatalf("expected null numeric values to map to zero, got bytes=%d rows=%d", plain.TotalBytes, plain.TotalRows)
	}
}

func TestFetchTableMetadataQueryError(t *testing.T) {
	state := &mockState{
		columns:  []string{"database"},
		queryErr: errors.New("query failed"),
	}
	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	client := &ClickHouseClient{conn: db, config: config.DefaultConfig()}
	_, err := client.FetchTableMetadata(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to fetch table metadata") {
		t.Fatalf("expected metadata query error, got %v", err)
	}
}

func TestFetchTableMetadataAppliesExclusions(t *testing.T) {
	columns := []string{
		"database",
		"name",
		"engine",
		"total_bytes",
		"total_rows",
		"create_time",
		"dep_databases",
		"dep_tables",
	}

	createTime := time.Date(2026, 2, 16, 10, 0, 0, 0, time.UTC)
	state := &mockState{
		columns: columns,
		pages: [][][]driver.Value{
			{
				{driver.Value("db1"), driver.Value("keep"), driver.Value("MergeTree"), driver.Value(int64(1)), driver.Value(int64(1)), driver.Value(createTime), nil, nil},
				{driver.Value("db1"), driver.Value("tmp_stage"), driver.Value("MergeTree"), driver.Value(int64(1)), driver.Value(int64(1)), driver.Value(createTime), nil, nil},
				{driver.Value("tmpdb"), driver.Value("sessions"), driver.Value("MergeTree"), driver.Value(int64(1)), driver.Value(int64(1)), driver.Value(createTime), nil, nil},
			},
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := config.DefaultConfig()
	cfg.ExcludeTables = []string{"db1.tmp_*"}
	cfg.ExcludeDatabases = []string{"tmp*"}
	cfg.Normalize()

	client := &ClickHouseClient{conn: db, config: cfg}
	tables, err := client.FetchTableMetadata(context.Background())
	if err != nil {
		t.Fatalf("FetchTableMetadata failed: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected 1 table after exclusions, got %d", len(tables))
	}
	if _, found := tables["db1.keep"]; !found {
		t.Fatalf("expected db1.keep to remain, got %v", tables)
	}
}

func TestClickHouseClientAndCollectorClose(t *testing.T) {
	client := &ClickHouseClient{}
	if err := client.Close(); err != nil {
		t.Fatalf("expected nil close error for nil conn, got %v", err)
	}

	state := &mockState{columns: testQueryLogColumns()}
	db := newMockDB(t, state)
	client = &ClickHouseClient{conn: db, config: config.DefaultConfig()}
	if err := client.Close(); err != nil {
		t.Fatalf("expected close success, got %v", err)
	}

	col := &collector{client: nil}
	if err := col.Close(); err != nil {
		t.Fatalf("expected collector close success, got %v", err)
	}
}

func TestCollectorNewInvalidDSN(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ClickHouseDSN = "://invalid"

	_, err := New(cfg)
	if err == nil || !strings.Contains(err.Error(), "failed to create ClickHouse client") {
		t.Fatalf("expected New error for invalid DSN, got %v", err)
	}
}

func TestCollectorCollectAndWrapperMethods(t *testing.T) {
	state := &mockState{
		columns: testQueryLogColumns(),
		pages: [][][]driver.Value{
			{testQueryRow("q1", "SELECT * FROM db.table1", 100)},
		},
	}
	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := config.DefaultConfig()
	cfg.BatchSize = 10
	cfg.MaxRows = 100
	cfg.LookbackPeriod = 24 * time.Hour

	client := &ClickHouseClient{conn: db, config: cfg}
	col := &collector{
		config: cfg,
		client: client,
		pool:   NewWorkerPool(2),
	}

	entries, err := col.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if col.pool.started {
		t.Fatal("expected worker pool to be stopped after Collect")
	}

	// Wrapper method coverage.
	metadataState := &mockState{
		columns: []string{"database", "name", "engine", "total_bytes", "total_rows", "create_time", "dep_databases", "dep_tables"},
		pages: [][][]driver.Value{
			{
				{driver.Value("db"), driver.Value("tbl"), driver.Value("MergeTree"), driver.Value(int64(1)), driver.Value(int64(1)), driver.Value(time.Now()), driver.Value(""), driver.Value("")},
			},
		},
	}
	metadataDB := newMockDB(t, metadataState)
	t.Cleanup(func() {
		_ = metadataDB.Close()
	})
	col.client = &ClickHouseClient{conn: metadataDB, config: cfg}

	tables, err := col.FetchTableMetadata(context.Background())
	if err != nil {
		t.Fatalf("collector FetchTableMetadata failed: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected 1 table from wrapper, got %d", len(tables))
	}
}

func TestCollectorCollectErrorPathStopsPool(t *testing.T) {
	state := &mockState{
		columns:  testQueryLogColumns(),
		queryErr: errors.New("boom"),
	}
	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := config.DefaultConfig()
	cfg.BatchSize = 10
	cfg.MaxRows = 100
	cfg.LookbackPeriod = 24 * time.Hour

	col := &collector{
		config: cfg,
		client: &ClickHouseClient{conn: db, config: cfg},
		pool:   NewWorkerPool(1),
	}

	_, err := col.Collect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to fetch query logs") {
		t.Fatalf("expected collect error, got %v", err)
	}
	if col.pool.started {
		t.Fatal("expected worker pool to be stopped on error")
	}
}

func TestWorkerPoolLifecycle(t *testing.T) {
	pool := NewWorkerPool(2)
	if pool.Results() == nil || pool.Errors() == nil {
		t.Fatal("expected results and errors channels")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Start(ctx)
	pool.Start(ctx) // Idempotent.

	pool.Submit(&models.QueryLogEntry{QueryID: "q1"})
	pool.Submit(&models.QueryLogEntry{QueryID: "q2"})
	pool.Stop()

	var got []string
	for entry := range pool.Results() {
		got = append(got, entry.QueryID)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d (%v)", len(got), got)
	}
	if pool.started {
		t.Fatal("expected pool started=false after Stop")
	}
}

func TestWorkerPoolStopBeforeStartAndSubmitAfterCancel(t *testing.T) {
	pool := NewWorkerPool(1)
	pool.Stop() // No-op path.

	ctx, cancel := context.WithCancel(context.Background())
	pool.Start(ctx)
	cancel()
	pool.Submit(&models.QueryLogEntry{QueryID: "ignored"})
	pool.Stop()
}
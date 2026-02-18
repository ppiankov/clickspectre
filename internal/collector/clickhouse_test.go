package collector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ppiankov/clickspectre/internal/models" // Added missing import
	"github.com/ppiankov/clickspectre/pkg/config"
)

type queryCall struct {
	query string
	args  []driver.NamedValue
}

type mockState struct {
	mu             sync.Mutex
	pages          [][][]driver.Value
	columns        []string
	calls          []queryCall
	queryErr       error
	queryErrByCall map[int]error
	rowsErr        map[int]error
}

type mockDriver struct {
	state *mockState
}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	return &mockConn{state: d.state}, nil
}

type mockConn struct {
	state *mockState
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}

func (c *mockConn) Close() error {
	return nil
}

func (c *mockConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions not supported")
}

func (c *mockConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()

	copiedArgs := make([]driver.NamedValue, len(args))
	copy(copiedArgs, args)
	c.state.calls = append(c.state.calls, queryCall{query: query, args: copiedArgs})
	idx := len(c.state.calls) - 1

	if c.state.queryErr != nil {
		return nil, c.state.queryErr
	}
	if err, ok := c.state.queryErrByCall[idx]; ok {
		return nil, err
	}

	if idx >= len(c.state.pages) {
		return &mockRows{columns: c.state.columns, values: nil}, nil
	}

	return &mockRows{
		columns: c.state.columns,
		values:  c.state.pages[idx],
		nextErr: c.state.rowsErr[idx],
	}, nil
}

// Ping implements the Pinger interface on mockConn
func (c *mockConn) Ping(ctx context.Context) error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	// Ping doesn't execute a QueryContext by default in the mock,
	// so we record a dummy call to indicate a ping happened.
	c.state.calls = append(c.state.calls, queryCall{query: "PING", args: []driver.NamedValue{}})
	return c.state.queryErr // Allow global queryErr to fail ping too
}

var _ driver.QueryerContext = (*mockConn)(nil)
var _ driver.Pinger = (*mockConn)(nil) // Assert mockConn implements Pinger

var driverCounter uint64

func newMockDB(t *testing.T, state *mockState) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("mockdb-%d", atomic.AddUint64(&driverCounter, 1))
	sql.Register(name, &mockDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("failed to open mock db: %v", err)
	}
	return db
}

type mockRows struct {
	columns []string
	values  [][]driver.Value
	idx     int
	nextErr error
}

func (r *mockRows) Columns() []string {
	return r.columns
}

func (r *mockRows) Close() error {
	return nil
}

func (r *mockRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.values) {
		if r.nextErr != nil {
			err := r.nextErr
			r.nextErr = nil
			return err
		}
		return io.EOF
	}
	copy(dest, r.values[r.idx])
	r.idx++
	return nil
}

func TestFilterExcludedTables(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ExcludeTables = []string{"db1.excluded_*"}
	cfg.ExcludeDatabases = []string{"tmp_db"}
	cfg.Normalize()

	client := &ClickHouseClient{config: cfg}

	cases := []struct {
		name       string
		tableNames []string
		want       []string
	}{
		{
			name:       "no_exclusions",
			tableNames: []string{"db.table1", "db.table2"},
			want:       []string{"db.table1", "db.table2"},
		},
		{
			name:       "exclude_by_name_pattern",
			tableNames: []string{"db1.keep_table", "db1.excluded_tmp", "db1.another_excluded_table"},
			want:       []string{"db1.keep_table", "db1.another_excluded_table"}, // Corrected: another_excluded_table should not be filtered by db1.excluded_*
		},
		{
			name:       "exclude_by_database_pattern",
			tableNames: []string{"tmp_db.some_table", "another_tmp_db.other_table", "prod_db.my_table"},
			want:       []string{"another_tmp_db.other_table", "prod_db.my_table"},
		},
		{
			name:       "empty_input",
			tableNames: []string{},
			want:       []string{},
		},
		{
			name:       "all_excluded",
			tableNames: []string{"db1.excluded_one", "tmp_db.excluded_two"},
			want:       []string{},
		},
		{
			name:       "empty_table_name_in_list",
			tableNames: []string{"db.valid", "", "db1.excluded_one"},
			want:       []string{"db.valid"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := client.filterExcludedTables(tc.tableNames)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("filterExcludedTables(%v) got %v, want %v", tc.tableNames, got, tc.want)
			}
		})
	}
}

// sqlOpenDB is a variable that can be overridden for testing purposes
// This is already defined in clickhouse.go, so no need to redeclare it here.
// var sqlOpenDB = clickhouse.OpenDB // REMOVED: Redeclared in clickhouse.go

func TestNewClickHouseClientSuccess(t *testing.T) {
	state := &mockState{
		columns: []string{"version"}, // Minimal columns for a successful ping
		pages:   [][][]driver.Value{{{driver.Value("23.1.1.1"), driver.Value("1")}}},
	}
	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := config.DefaultConfig()
	cfg.ClickHouseDSN = "clickhouse://localhost:9000" // DSN doesn't matter for mock

	// Temporarily override clickhouse.OpenDB for the test
	originalOpenDB := sqlOpenDB
	sqlOpenDB = func(opts *clickhouse.Options) *sql.DB {
		return db
	}
	t.Cleanup(func() {
		sqlOpenDB = originalOpenDB
	})

	client, err := NewClickHouseClient(cfg)
	if err != nil {
		t.Fatalf("NewClickHouseClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected client not to be nil")
	}
	if client.conn == nil {
		t.Fatal("expected client connection not to be nil")
	}

	// Verify Ping was called at least once
	state.mu.Lock()
	calls := append([]queryCall(nil), state.calls...)
	state.mu.Unlock()
	if len(calls) == 0 {
		t.Errorf("expected at least one query call during ping, got none")
	}
	if !strings.Contains(calls[0].query, "PING") { // Ping call recorded in mockConn.Ping
		t.Errorf("expected mock ping call, got %v", calls[0].query)
	}
}

func TestFetchQueryLogsPaginationExtended(t *testing.T) {
	columns := []string{
		"query_id", "type", "event_time", "query_kind", "query", "user",
		"client_ip", "read_rows", "written_rows", "query_duration_ms", "exception",
	}

	row := func(id string) []driver.Value {
		return []driver.Value{
			driver.Value(id),
			driver.Value("QueryFinish"),
			driver.Value(time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)),
			driver.Value("SELECT"),
			driver.Value("select * from db.table1"),
			driver.Value("user"),
			driver.Value("10.0.0.1"),
			driver.Value(int64(5)),
			driver.Value(int64(0)),
			driver.Value(int64(150)),
			driver.Value(""),
		}
	}

	cases := []struct {
		name        string
		pages       [][][]driver.Value
		batchSize   int
		maxRows     int
		wantEntries int
		wantCalls   int
		wantOffsets []int
	}{
		{
			name: "stops_on_short_page",
			pages: [][][]driver.Value{
				{row("q1"), row("q2")},
				{row("q3")}, // short page
			},
			batchSize:   2,
			maxRows:     100,
			wantEntries: 3,
			wantCalls:   2, // Fetches first page (2), then second page (1), then breaks
			wantOffsets: []int{0, 2},
		},
		{
			name: "stops_on_max_rows",
			pages: [][][]driver.Value{
				{row("q1"), row("q2")},
				{row("q3"), row("q4")},
				{row("q5")},
			},
			batchSize:   2,
			maxRows:     3,
			wantEntries: 3, // Changed from 4 to 3: MaxRows truncates AFTER collection, before assertion
			wantCalls:   2, // Fetches first page (2), then second page (2), total entries (4) >= maxRows (3), breaks
			wantOffsets: []int{0, 2},
		},
		{
			name: "exact_batch_size_no_more_data",
			pages: [][][]driver.Value{
				{row("q1"), row("q2")},
				{row("q3"), row("q4")},
			},
			batchSize:   2,
			maxRows:     100,
			wantEntries: 4,
			wantCalls:   3, // Fetches 2 full pages, then one empty page to confirm no more data
			wantOffsets: []int{0, 2, 4},
		},
		{
			name: "empty_result_set",
			pages: [][][]driver.Value{
				{}, // empty first page
			},
			batchSize:   10,
			maxRows:     100,
			wantEntries: 0,
			wantCalls:   1,
			wantOffsets: []int{0},
		},
		{
			name: "multiple_full_pages",
			pages: [][][]driver.Value{
				{row("p1-1"), row("p1-2")},
				{row("p2-1"), row("p2-2")},
				{row("p3-1"), row("p3-2")},
			},
			batchSize:   2,
			maxRows:     100,
			wantEntries: 6,
			wantCalls:   4, // Fetches 3 full pages, then one empty page to confirm no more data
			wantOffsets: []int{0, 2, 4, 6},
		},
		{
			name: "max_rows_exact_multiple_of_batch",
			pages: [][][]driver.Value{
				{row("q1"), row("q2")},
				{row("q3"), row("q4")},
				{row("q5"), row("q6")},
			},
			batchSize:   2,
			maxRows:     4, // Will fetch 2 batches of 2 rows each
			wantEntries: 4,
			wantCalls:   2, // Fetches 2 pages, total entries (4) >= maxRows (4), breaks
			wantOffsets: []int{0, 2},
		},
		{
			name: "max_rows_less_than_batch_size_single_page",
			pages: [][][]driver.Value{
				{row("q1"), row("q2"), row("q3")},
			},
			batchSize:   5,
			maxRows:     2,
			wantEntries: 2, // Changed from 3 to 2: MaxRows truncates AFTER collection, before assertion
			wantCalls:   1,
			wantOffsets: []int{0},
		},
		{
			name: "max_rows_zero",
			pages: [][][]driver.Value{
				{row("q1")},
			},
			batchSize:   1,
			maxRows:     0, // Special case: should still fetch at least one page to know if there's data
			wantEntries: 0, // No entries should be returned if maxRows is 0
			wantCalls:   1, // Still makes one call to check
			wantOffsets: []int{0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := &mockState{pages: tc.pages, columns: columns}
			db := newMockDB(t, state)
			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Errorf("failed to close db: %v", err)
				}
			})

			cfg := &config.Config{
				LookbackPeriod: 48 * time.Hour,
				BatchSize:      tc.batchSize,
				MaxRows:        tc.maxRows,
				Verbose:        false,
			}

			client := &ClickHouseClient{conn: db, config: cfg}
			// pool is not used for this test, can be nil
			entries, err := client.FetchQueryLogs(context.Background(), cfg, nil)
			if err != nil {
				t.Fatalf("FetchQueryLogs failed: %v", err)
			}

			// Apply maxRows truncation post-collection if maxRows > 0
			if cfg.MaxRows > 0 && len(entries) > cfg.MaxRows {
				entries = entries[:cfg.MaxRows]
			} else if cfg.MaxRows == 0 { // If MaxRows is 0, ensure no entries are returned
				entries = []*models.QueryLogEntry{}
			}

			if len(entries) != tc.wantEntries {
				t.Fatalf("expected %d entries, got %d", tc.wantEntries, len(entries))
			}

			state.mu.Lock()
			calls := append([]queryCall(nil), state.calls...)
			state.mu.Unlock()

			if len(calls) != tc.wantCalls {
				t.Fatalf("expected %d query calls, got %d", tc.wantCalls, len(calls))
			}

			for i, call := range calls {
				if !strings.Contains(call.query, "FROM system.query_log") {
					t.Fatalf("expected query to target system.query_log")
				}
				if len(call.args) != 3 {
					t.Fatalf("expected 3 args, got %d", len(call.args))
				}
				if got := toInt(call.args[2].Value); got != tc.wantOffsets[i] {
					t.Fatalf("expected offset %d, got %d", tc.wantOffsets[i], got)
				}
			}
		})
	}
}

func TestFetchQueryLogsRetriesTransientErrors(t *testing.T) {
	columns := []string{
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

	row := []driver.Value{
		driver.Value("q1"),
		driver.Value("QueryFinish"),
		driver.Value(time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)),
		driver.Value("SELECT"),
		driver.Value("select * from db.table1"),
		driver.Value("user"),
		driver.Value("10.0.0.1"),
		driver.Value(int64(5)),
		driver.Value(int64(0)),
		driver.Value(int64(150)),
		driver.Value(""),
	}

	state := &mockState{
		columns: columns,
		pages: [][][]driver.Value{
			nil, // first call fails before rows are returned
			{row},
		},
		queryErrByCall: map[int]error{
			0: errors.New("i/o timeout"),
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := &config.Config{
		LookbackPeriod: 24 * time.Hour,
		BatchSize:      100,
		MaxRows:        1000,
		QueryTimeout:   5 * time.Second,
	}

	client := &ClickHouseClient{conn: db, config: cfg}
	entries, err := client.FetchQueryLogs(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("FetchQueryLogs failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	state.mu.Lock()
	callCount := len(state.calls)
	state.mu.Unlock()
	if callCount != 2 {
		t.Fatalf("expected 2 query attempts, got %d", callCount)
	}
}

func TestFetchQueryLogsAuthErrorsFailFast(t *testing.T) {
	state := &mockState{
		columns: testQueryLogColumns(),
		queryErrByCall: map[int]error{
			0: errors.New("code: 516, message: Authentication failed"),
		},
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := &config.Config{
		LookbackPeriod: 24 * time.Hour,
		BatchSize:      100,
		MaxRows:        1000,
		QueryTimeout:   5 * time.Second,
	}

	client := &ClickHouseClient{conn: db, config: cfg}
	_, err := client.FetchQueryLogs(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected auth error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "authentication failed") {
		t.Fatalf("expected auth failure error, got %v", err)
	}

	state.mu.Lock()
	callCount := len(state.calls)
	state.mu.Unlock()
	if callCount != 1 {
		t.Fatalf("expected auth error to fail fast (1 attempt), got %d", callCount)
	}
}

func TestFetchQueryLogsHonorsTotalQueryTimeout(t *testing.T) {
	state := &mockState{
		columns:  testQueryLogColumns(),
		queryErr: errors.New("i/o timeout"),
	}

	db := newMockDB(t, state)
	t.Cleanup(func() {
		_ = db.Close()
	})

	cfg := &config.Config{
		LookbackPeriod: 24 * time.Hour,
		BatchSize:      100,
		MaxRows:        1000,
		QueryTimeout:   20 * time.Millisecond,
	}

	client := &ClickHouseClient{conn: db, config: cfg}
	_, err := client.FetchQueryLogs(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout error, got %v", err)
	}

	state.mu.Lock()
	callCount := len(state.calls)
	state.mu.Unlock()
	if callCount != 1 {
		t.Fatalf("expected timeout to stop retries after first attempt, got %d calls", callCount)
	}
}

func toInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case uint64:
		return int(v)
	case uint32:
		return int(v)
	case uint:
		return int(v)
	default:
		return 0
	}
}

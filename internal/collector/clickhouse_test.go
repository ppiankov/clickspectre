package collector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

var _ driver.QueryerContext = (*mockConn)(nil)

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

func TestExtractTables(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "from_clause",
			query: "SELECT * FROM db.table1",
			want:  []string{"db.table1"},
		},
		{
			name:  "join_clause",
			query: "select * from table1 join db.table2 on table1.id = table2.id",
			want:  []string{"table1", "db.table2"},
		},
		{
			name:  "insert_into",
			query: "INSERT INTO db.table3 values (1)",
			want:  []string{"db.table3"},
		},
		{
			name:  "create_table",
			query: "CREATE TABLE IF NOT EXISTS table4 (id UInt64)",
			want:  []string{"table4"},
		},
		{
			name:  "create_or_replace",
			query: "CREATE OR REPLACE TABLE db.table5 (id UInt64)",
			want:  []string{"db.table5"},
		},
		{
			name:  "dedup",
			query: "SELECT * FROM db.table6 JOIN db.table6 on 1=1",
			want:  []string{"db.table6"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractTables(tc.query)
			sort.Strings(got)
			sort.Strings(tc.want)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestFetchQueryLogsPagination(t *testing.T) {
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

	row := func(id string) []driver.Value {
		return []driver.Value{
			id,
			"QueryFinish",
			time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			"SELECT",
			"select * from db.table1",
			"user",
			"10.0.0.1",
			int64(5),
			int64(0),
			int64(150),
			"",
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
				{row("q3")},
			},
			batchSize:   2,
			maxRows:     100,
			wantEntries: 3,
			wantCalls:   2,
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
			wantEntries: 4,
			wantCalls:   2,
			wantOffsets: []int{0, 2},
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
			entries, err := client.FetchQueryLogs(context.Background(), cfg, nil)
			if err != nil {
				t.Fatalf("FetchQueryLogs failed: %v", err)
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
		"q1",
		"QueryFinish",
		time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC),
		"SELECT",
		"select * from db.table1",
		"user",
		"10.0.0.1",
		int64(5),
		int64(0),
		int64(150),
		"",
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

package scorer

import (
	"math"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func TestSimpleScorerScore(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		table    *models.Table
		services map[string]*models.Service
		want     float64
	}{
		{
			name: "recent_high_volume_many_services_with_writes",
			table: &models.Table{
				FullName:   "db.table1",
				Reads:      2000,
				Writes:     5,
				LastAccess: now.Add(-48 * time.Hour),
			},
			services: servicesUsingTable("db.table1", 6),
			want:     1.0,
		},
		{
			name: "moderate_usage_low_diversity_no_writes",
			table: &models.Table{
				FullName:   "db.table2",
				Reads:      50,
				Writes:     0,
				LastAccess: now.Add(-20 * 24 * time.Hour),
			},
			services: servicesUsingTable("db.table2", 2),
			want:     0.45,
		},
		{
			name: "old_low_usage_no_services",
			table: &models.Table{
				FullName:   "db.table3",
				Reads:      0,
				Writes:     0,
				LastAccess: now.Add(-120 * 24 * time.Hour),
			},
			services: map[string]*models.Service{},
			want:     0.0,
		},
		{
			name: "zero_usage_mv_replicated_small",
			table: &models.Table{
				FullName:     "db.table4",
				ZeroUsage:    true,
				IsMV:         true,
				IsReplicated: true,
				TotalBytes:   500000,
			},
			services: map[string]*models.Service{},
			want:     1.0,
		},
		{
			name: "zero_usage_replicated_large",
			table: &models.Table{
				FullName:     "db.table5",
				ZeroUsage:    true,
				IsReplicated: true,
				TotalBytes:   2 * 1e9,
			},
			services: map[string]*models.Service{},
			want:     0.20,
		},
	}

	scorer := &SimpleScorer{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scorer.Score(tc.table, tc.services)
			if math.Abs(got-tc.want) > 0.0001 {
				t.Fatalf("expected score %.2f, got %.2f", tc.want, got)
			}
		})
	}
}

func TestSimpleScorerCategorize(t *testing.T) {
	cases := []struct {
		name  string
		score float64
		want  string
	}{
		{name: "active", score: 0.70, want: "active"},
		{name: "suspect_lower", score: 0.30, want: "suspect"},
		{name: "unused", score: 0.10, want: "unused"},
	}

	scorer := &SimpleScorer{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scorer.Categorize(tc.score); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestCountServicesUsingTable(t *testing.T) {
	cases := []struct {
		name     string
		table    string
		services map[string]*models.Service
		want     int
	}{
		{
			name:  "counts_unique_services",
			table: "db.table1",
			services: map[string]*models.Service{
				"svc1": {TablesUsed: []string{"db.table1", "db.table2"}},
				"svc2": {TablesUsed: []string{"db.table1", "db.table1"}},
				"svc3": {TablesUsed: []string{"db.table3"}},
			},
			want: 2,
		},
		{
			name:     "no_services",
			table:    "db.table1",
			services: map[string]*models.Service{},
			want:     0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countServicesUsingTable(tc.table, tc.services); got != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, got)
			}
		})
	}
}

func TestGenerateRecommendations(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		tables   map[string]*models.Table
		services map[string]*models.Service
		cfg      *config.Config
		verify   func(t *testing.T, recs models.CleanupRecommendations)
	}{
		{
			name: "categorizes_tables_and_zero_usage",
			tables: map[string]*models.Table{
				"db.zero_nonrep": {
					Name:       "zero_nonrep",
					Database:   "db",
					FullName:   "db.zero_nonrep",
					ZeroUsage:  true,
					TotalBytes: 2 * 1e6,
				},
				"db.zero_rep": {
					Name:         "zero_rep",
					Database:     "db",
					FullName:     "db.zero_rep",
					ZeroUsage:    true,
					IsReplicated: true,
					TotalBytes:   2 * 1e9,
				},
				"db.small_skip": {
					Name:       "small_skip",
					Database:   "db",
					FullName:   "db.small_skip",
					ZeroUsage:  true,
					TotalBytes: 100,
				},
				"system.query_log": {
					Name:       "query_log",
					Database:   "system",
					FullName:   "system.query_log",
					Reads:      0,
					Writes:     0,
					LastAccess: now.Add(-90 * 24 * time.Hour),
				},
				"db.recent_write": {
					Name:       "recent_write",
					Database:   "db",
					FullName:   "db.recent_write",
					Reads:      0,
					Writes:     10,
					LastAccess: now.Add(-24 * time.Hour),
				},
				"db.unused": {
					Name:       "unused",
					Database:   "db",
					FullName:   "db.unused",
					Reads:      0,
					Writes:     0,
					LastAccess: now.Add(-120 * 24 * time.Hour),
				},
				"db.suspect": {
					Name:       "suspect",
					Database:   "db",
					FullName:   "db.suspect",
					Reads:      50,
					Writes:     0,
					LastAccess: now.Add(-20 * 24 * time.Hour),
				},
				"db.active": {
					Name:       "active",
					Database:   "db",
					FullName:   "db.active",
					Reads:      2000,
					Writes:     1,
					LastAccess: now.Add(-48 * time.Hour),
				},
			},
			services: servicesUsingTable("db.active", 6),
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.MinTableSizeMB = 1.0
				cfg.Verbose = false
				return cfg
			}(),
			verify: func(t *testing.T, recs models.CleanupRecommendations) {
				if len(recs.ZeroUsageNonReplicated) != 1 || recs.ZeroUsageNonReplicated[0].Name != "db.zero_nonrep" {
					t.Fatalf("expected zero usage non-replicated recommendation for db.zero_nonrep")
				}
				if len(recs.ZeroUsageReplicated) != 1 || recs.ZeroUsageReplicated[0].Name != "db.zero_rep" {
					t.Fatalf("expected zero usage replicated recommendation for db.zero_rep")
				}
				if !containsString(recs.SafeToDrop, "db.unused") {
					t.Fatalf("expected db.unused in safe_to_drop")
				}
				if !containsString(recs.LikelySafe, "db.suspect") {
					t.Fatalf("expected db.suspect in likely_safe")
				}
				if !containsString(recs.Keep, "db.active") {
					t.Fatalf("expected db.active in keep")
				}
				if !containsString(recs.Keep, "system.query_log") {
					t.Fatalf("expected system.query_log in keep")
				}
				if !containsString(recs.Keep, "db.recent_write") {
					t.Fatalf("expected db.recent_write in keep")
				}
			},
		},
		{
			name: "min_query_count_marks_low_query_tables_likely_safe",
			tables: map[string]*models.Table{
				"db.low_queries": {
					Name:       "low_queries",
					Database:   "db",
					FullName:   "db.low_queries",
					Reads:      1500,
					LastAccess: now.Add(-10 * 24 * time.Hour),
					Sparkline: []models.TimeSeriesPoint{
						{Timestamp: now.Add(-4 * time.Hour), Value: 2},
					},
				},
				"db.high_queries": {
					Name:       "high_queries",
					Database:   "db",
					FullName:   "db.high_queries",
					Reads:      1500,
					LastAccess: now.Add(-10 * 24 * time.Hour),
					Sparkline: []models.TimeSeriesPoint{
						{Timestamp: now.Add(-4 * time.Hour), Value: 20},
					},
				},
			},
			services: map[string]*models.Service{
				"svcA": {TablesUsed: []string{"db.low_queries", "db.high_queries"}},
				"svcB": {TablesUsed: []string{"db.low_queries", "db.high_queries"}},
				"svcC": {TablesUsed: []string{"db.low_queries", "db.high_queries"}},
				"svcD": {TablesUsed: []string{"db.low_queries", "db.high_queries"}},
				"svcE": {TablesUsed: []string{"db.low_queries", "db.high_queries"}},
				"svcF": {TablesUsed: []string{"db.low_queries", "db.high_queries"}},
			},
			cfg: func() *config.Config {
				cfg := config.DefaultConfig()
				cfg.MinQueryCount = 10
				return cfg
			}(),
			verify: func(t *testing.T, recs models.CleanupRecommendations) {
				if !containsString(recs.LikelySafe, "db.low_queries") {
					t.Fatalf("expected db.low_queries in likely_safe")
				}
				if !containsString(recs.Keep, "db.high_queries") {
					t.Fatalf("expected db.high_queries in keep")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recs := GenerateRecommendations(tc.tables, tc.services, tc.cfg)
			tc.verify(t, recs)
		})
	}
}

func servicesUsingTable(table string, count int) map[string]*models.Service {
	services := make(map[string]*models.Service)
	for i := 0; i < count; i++ {
		name := "svc" + string(rune('A'+i))
		services[name] = &models.Service{TablesUsed: []string{table}}
	}
	return services
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

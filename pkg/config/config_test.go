package config

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	cases := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{name: "QueryTimeout", got: cfg.QueryTimeout, want: 5 * time.Minute},
		{name: "BatchSize", got: cfg.BatchSize, want: 100000},
		{name: "MaxRows", got: cfg.MaxRows, want: 1000000},
		{name: "LookbackPeriod", got: cfg.LookbackPeriod, want: 30 * 24 * time.Hour},
		{name: "MinQueryCount", got: cfg.MinQueryCount, want: uint64(0)},
		{name: "ExcludeTables", got: len(cfg.ExcludeTables), want: 0},
		{name: "ExcludeDatabases", got: len(cfg.ExcludeDatabases), want: 0},
		{name: "ResolveK8s", got: cfg.ResolveK8s, want: false},
		{name: "K8sCacheTTL", got: cfg.K8sCacheTTL, want: 5 * time.Minute},
		{name: "K8sRateLimit", got: cfg.K8sRateLimit, want: 10},
		{name: "Concurrency", got: cfg.Concurrency, want: 5},
		{name: "OutputDir", got: cfg.OutputDir, want: "./report"},
		{name: "Format", got: cfg.Format, want: "json"},
		{name: "BaselinePath", got: cfg.BaselinePath, want: ""},
		{name: "UpdateBaseline", got: cfg.UpdateBaseline, want: false},
		{name: "ScoringAlgorithm", got: cfg.ScoringAlgorithm, want: "simple"},
		{name: "AnomalyDetection", got: cfg.AnomalyDetection, want: true},
		{name: "IncludeMVDeps", got: cfg.IncludeMVDeps, want: true},
		{name: "DetectUnusedTables", got: cfg.DetectUnusedTables, want: false},
		{name: "MinTableSizeMB", got: cfg.MinTableSizeMB, want: 1.0},
		{name: "ServerPort", got: cfg.ServerPort, want: 8080},
		{name: "Verbose", got: cfg.Verbose, want: false},
		{name: "DryRun", got: cfg.DryRun, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, tc.got)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "seconds", input: "30s", want: 30 * time.Second},
		{name: "minutes", input: "5m", want: 5 * time.Minute},
		{name: "hours", input: "2h", want: 2 * time.Hour},
		{name: "days", input: "7d", want: 7 * 24 * time.Hour},
		{name: "fallback_go_duration", input: "1.5h", want: time.Duration(1.5 * float64(time.Hour))},
		{name: "invalid", input: "5x", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDuration(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

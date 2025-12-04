package config

import "time"

// Config holds all runtime configuration
type Config struct {
	// ClickHouse settings
	ClickHouseDSN  string
	QueryTimeout   time.Duration
	BatchSize      int
	MaxRows        int
	LookbackPeriod time.Duration

	// Kubernetes settings
	ResolveK8s   bool
	KubeConfig   string
	K8sCacheTTL  time.Duration
	K8sRateLimit int

	// Concurrency settings
	Concurrency int

	// Output settings
	OutputDir string
	Format    string

	// Analysis settings
	ScoringAlgorithm string
	AnomalyDetection bool
	IncludeMVDeps    bool

	// Server settings
	ServerPort int

	// Operational flags
	Verbose bool
	DryRun  bool
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		QueryTimeout:     5 * time.Minute,
		BatchSize:        100000,
		MaxRows:          1000000,
		LookbackPeriod:   30 * 24 * time.Hour, // 30 days
		ResolveK8s:       false,
		K8sCacheTTL:      5 * time.Minute,
		K8sRateLimit:     10,
		Concurrency:      5,
		OutputDir:        "./report",
		Format:           "json",
		ScoringAlgorithm: "simple",
		AnomalyDetection: true,
		IncludeMVDeps:    true,
		ServerPort:       8080,
		Verbose:          false,
		DryRun:           false,
	}
}

package models

import "time"

// Report is the complete output structure
type Report struct {
	Tool                   string                 `json:"tool"`
	Version                string                 `json:"version"`
	Timestamp              string                 `json:"timestamp"`
	Metadata               Metadata               `json:"metadata"`
	Collection             *CollectionMeta        `json:"collection,omitempty"`
	Tables                 []Table                `json:"tables"`
	Services               []Service              `json:"services"`
	Edges                  []Edge                 `json:"edges"`
	Anomalies              []Anomaly              `json:"anomalies"`
	Users                  []UserActivity         `json:"users,omitempty"`
	CleanupRecommendations CleanupRecommendations `json:"cleanup_recommendations"`
}

// CollectionMeta holds metadata about the query_log collection across nodes.
type CollectionMeta struct {
	Nodes        []string `json:"nodes"`
	FailedNodes  []string `json:"failed_nodes"`
	TotalEntries int      `json:"total_entries"`
	Deduplicated int      `json:"deduplicated,omitempty"`
}

// Metadata contains report generation info
type Metadata struct {
	GeneratedAt          time.Time `json:"generated_at"`
	LookbackDays         int       `json:"lookback_days"`
	ClickHouseHost       string    `json:"clickhouse_host"`
	TotalQueriesAnalyzed uint64    `json:"total_queries_analyzed"`
	AnalysisDuration     string    `json:"analysis_duration"`
	Version              string    `json:"version"`
	K8sResolutionEnabled bool      `json:"k8s_resolution_enabled"`
}

// CleanupRecommendations groups tables by safety category
type CleanupRecommendations struct {
	ZeroUsageNonReplicated []TableRecommendation `json:"zero_usage_non_replicated"` // High priority - unused, not replicated
	ZeroUsageReplicated    []TableRecommendation `json:"zero_usage_replicated"`     // Lower priority - unused, replicated
	SafeToDrop             []string              `json:"safe_to_drop"`
	LikelySafe             []string              `json:"likely_safe"`
	Keep                   []string              `json:"keep"`
}

// TableRecommendation contains detailed information about a table for cleanup recommendations
type TableRecommendation struct {
	Name         string  `json:"name"`
	Database     string  `json:"database"`
	Engine       string  `json:"engine"`
	IsReplicated bool    `json:"is_replicated"`
	SizeMB       float64 `json:"size_mb"`
	Rows         uint64  `json:"rows"`
}

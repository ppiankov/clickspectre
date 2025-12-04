package models

import "time"

// Report is the complete output structure
type Report struct {
	Metadata               Metadata               `json:"metadata"`
	Tables                 []Table                `json:"tables"`
	Services               []Service              `json:"services"`
	Edges                  []Edge                 `json:"edges"`
	Anomalies              []Anomaly              `json:"anomalies"`
	CleanupRecommendations CleanupRecommendations `json:"cleanup_recommendations"`
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
	SafeToDrop []string `json:"safe_to_drop"`
	LikelySafe []string `json:"likely_safe"`
	Keep       []string `json:"keep"`
}

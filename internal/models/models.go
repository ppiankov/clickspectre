package models

import "time"

// QueryLogEntry represents a single entry from system.query_log
type QueryLogEntry struct {
	QueryID     string
	Type        string // 'QueryStart', 'QueryFinish', etc.
	EventTime   time.Time
	QueryKind   string // 'Select', 'Insert', 'Create', etc.
	Query       string
	User        string
	ClientIP    string
	ReadRows    uint64
	WrittenRows uint64
	Duration    time.Duration
	Exception   string
	Tables      []string // Extracted from query
}

// Table represents a ClickHouse table with usage stats
type Table struct {
	Name         string            `json:"name"`
	Database     string            `json:"database"`
	FullName     string            `json:"full_name"` // "db.table"
	Reads        uint64            `json:"reads"`
	Writes       uint64            `json:"writes"`
	LastAccess   time.Time         `json:"last_access"`
	FirstSeen    time.Time         `json:"first_seen"`
	Sparkline    []TimeSeriesPoint `json:"sparkline"`
	Score        float64           `json:"score"`
	Category     string            `json:"category"` // "active", "unused", "suspect"
	IsMV         bool              `json:"is_materialized_view"`
	MVDependency []string          `json:"mv_dependencies,omitempty"`

	// New fields for unused table detection
	Engine       string    `json:"engine,omitempty"`      // "MergeTree", "ReplicatedMergeTree", etc.
	IsReplicated bool      `json:"is_replicated"`         // Derived from engine name
	TotalBytes   uint64    `json:"total_bytes,omitempty"` // Table size in bytes
	TotalRows    uint64    `json:"total_rows,omitempty"`  // Row count
	CreateTime   time.Time `json:"create_time,omitempty"` // Table creation time
	ZeroUsage    bool      `json:"zero_usage"`            // Flag: no queries in lookback period
}

// Service represents a Kubernetes service or raw IP
type Service struct {
	IP           string    `json:"ip"`
	K8sService   string    `json:"k8s_service,omitempty"`
	K8sNamespace string    `json:"k8s_namespace,omitempty"`
	K8sPod       string    `json:"k8s_pod,omitempty"`
	TablesUsed   []string  `json:"tables_used"`
	QueryCount   uint64    `json:"query_count"`
	LastSeen     time.Time `json:"last_seen"`
}

// Edge represents a Service→Table relationship
type Edge struct {
	ServiceIP    string    `json:"service"`
	ServiceName  string    `json:"service_name,omitempty"`
	TableName    string    `json:"table"`
	Reads        uint64    `json:"reads"`
	Writes       uint64    `json:"writes"`
	LastActivity time.Time `json:"last_activity"`
}

// Anomaly represents unusual access patterns
type Anomaly struct {
	Type            string    `json:"type"`
	Description     string    `json:"description"`
	Severity        string    `json:"severity"` // "low", "medium", "high"
	AffectedTable   string    `json:"affected_table,omitempty"`
	AffectedService string    `json:"affected_service,omitempty"`
	DetectedAt      time.Time `json:"detected_at"`
}

// TimeSeriesPoint for sparkline visualization
type TimeSeriesPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     uint64    `json:"value"`
}

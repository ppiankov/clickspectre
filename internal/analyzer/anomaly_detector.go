package analyzer

import (
	"log/slog"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
)

// detectAnomalies detects unusual access patterns
func (a *Analyzer) detectAnomalies() error {
	now := time.Now()

	for tableName, table := range a.tables {
		// Anomaly 1: Tables accessed only once
		totalAccess := table.Reads + table.Writes
		if totalAccess == 1 {
			a.anomalies = append(a.anomalies, &models.Anomaly{
				Type:          "single_access",
				Description:   "Table accessed only once in lookback period",
				Severity:      "low",
				AffectedTable: tableName,
				DetectedAt:    now,
			})
			continue
		}

		// Anomaly 2: Tables not accessed recently
		daysSinceAccess := now.Sub(table.LastAccess).Hours() / 24
		if daysSinceAccess > 30 {
			a.anomalies = append(a.anomalies, &models.Anomaly{
				Type:          "stale_table",
				Description:   "Table not accessed in over 30 days",
				Severity:      "medium",
				AffectedTable: tableName,
				DetectedAt:    now,
			})
		}

		// Anomaly 3: Write-only tables (no reads)
		if table.Writes > 0 && table.Reads == 0 {
			a.anomalies = append(a.anomalies, &models.Anomaly{
				Type:          "write_only",
				Description:   "Table has writes but no reads (possible data sink)",
				Severity:      "low",
				AffectedTable: tableName,
				DetectedAt:    now,
			})
		}

		// Anomaly 4: Read-only tables (no writes, might be outdated)
		if table.Reads > 100 && table.Writes == 0 {
			a.anomalies = append(a.anomalies, &models.Anomaly{
				Type:          "read_only",
				Description:   "Table has many reads but no writes (check if data is stale)",
				Severity:      "low",
				AffectedTable: tableName,
				DetectedAt:    now,
			})
		}

		// Anomaly 5: Tables with very few accesses (potential candidates for cleanup)
		if totalAccess < 10 && daysSinceAccess > 7 {
			a.anomalies = append(a.anomalies, &models.Anomaly{
				Type:          "low_activity",
				Description:   "Table has very low activity (< 10 accesses)",
				Severity:      "medium",
				AffectedTable: tableName,
				DetectedAt:    now,
			})
		}
	}

	// Service-level anomalies
	for serviceIP, service := range a.services {
		// Anomaly: Service accessing many tables (potential over-reach)
		if len(service.TablesUsed) > 20 {
			a.anomalies = append(a.anomalies, &models.Anomaly{
				Type:            "broad_access",
				Description:     "Service accesses many tables (> 20), check for over-privileged access",
				Severity:        "low",
				AffectedService: serviceIP,
				DetectedAt:      now,
			})
		}
	}

	slog.Debug("detected anomalies", slog.Int("count", len(a.anomalies)))

	return nil
}

package analyzer

import (
	"log/slog"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
)

// buildTableModel builds the table usage model from query log entries
func (a *Analyzer) buildTableModel(entries []*models.QueryLogEntry) error {
	for _, entry := range entries {
		for _, tableName := range entry.Tables {
			// Skip empty table names
			if tableName == "" {
				continue
			}
			if a.config.IsTableExcluded(tableName) {
				continue
			}

			// Get or create table
			table, exists := a.tables[tableName]
			if !exists {
				// Parse database.table format
				parts := strings.Split(tableName, ".")
				var database, name string
				if len(parts) == 2 {
					database = parts[0]
					name = parts[1]
				} else {
					database = ""
					name = tableName
				}

				table = &models.Table{
					Name:      name,
					Database:  database,
					FullName:  tableName,
					FirstSeen: entry.EventTime,
					Sparkline: make([]models.TimeSeriesPoint, 0),
				}
				a.tables[tableName] = table
			}

			// Update statistics based on query kind and actual row counts
			if isReadQuery(entry.QueryKind) {
				table.Reads += entry.ReadRows
			} else if isWriteQuery(entry.QueryKind) {
				table.Writes += entry.WrittenRows
			}

			// Update last access time
			if entry.EventTime.After(table.LastAccess) {
				table.LastAccess = entry.EventTime
			}

			// Update first seen time
			if entry.EventTime.Before(table.FirstSeen) {
				table.FirstSeen = entry.EventTime
			}
		}
	}

	if a.config.Verbose {
		slog.Debug("built table model", slog.Int("tables", len(a.tables)))
	}

	return nil
}

// generateSparklines generates time series data for sparkline visualization
func (a *Analyzer) generateSparklines(entries []*models.QueryLogEntry) error {
	// Group entries by table and hourly buckets
	type bucketKey struct {
		table string
		hour  int64 // Unix timestamp truncated to hour
	}

	buckets := make(map[bucketKey]uint64)

	for _, entry := range entries {
		hourTimestamp := entry.EventTime.Truncate(time.Hour).Unix()

		for _, tableName := range entry.Tables {
			if tableName == "" {
				continue
			}

			key := bucketKey{
				table: tableName,
				hour:  hourTimestamp,
			}
			buckets[key]++
		}
	}

	// Convert buckets to sparkline points
	for tableName, table := range a.tables {
		points := make([]models.TimeSeriesPoint, 0)

		// Find all time buckets for this table
		for key, count := range buckets {
			if key.table == tableName {
				points = append(points, models.TimeSeriesPoint{
					Timestamp: time.Unix(key.hour, 0),
					Value:     count,
				})
			}
		}

		// Sort points by timestamp
		// (Simple bubble sort for small datasets)
		for i := 0; i < len(points); i++ {
			for j := i + 1; j < len(points); j++ {
				if points[i].Timestamp.After(points[j].Timestamp) {
					points[i], points[j] = points[j], points[i]
				}
			}
		}

		table.Sparkline = points
	}

	if a.config.Verbose {
		slog.Debug("generated sparklines", slog.Int("tables", len(a.tables)))
	}

	return nil
}

// isReadQuery checks if a query kind is a read operation
func isReadQuery(kind string) bool {
	kind = strings.ToUpper(kind)
	return kind == "SELECT" || strings.HasPrefix(kind, "SELECT")
}

// isWriteQuery checks if a query kind is a write operation
func isWriteQuery(kind string) bool {
	kind = strings.ToUpper(kind)
	return kind == "INSERT" || kind == "CREATE" || kind == "DROP" ||
		kind == "ALTER" || kind == "UPDATE" || kind == "DELETE" ||
		strings.HasPrefix(kind, "INSERT") || strings.HasPrefix(kind, "CREATE")
}

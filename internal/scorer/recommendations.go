package scorer

import (
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

// GenerateRecommendations creates cleanup recommendations based on scores
func GenerateRecommendations(
	tables map[string]*models.Table,
	services map[string]*models.Service,
	config *config.Config,
) models.CleanupRecommendations {
	scorer := NewScorer(config.ScoringAlgorithm)

	// Initialize as empty slices instead of nil to avoid JSON null values
	zeroUsageNonReplicated := []models.TableRecommendation{}
	zeroUsageReplicated := []models.TableRecommendation{}
	safeToDrop := []string{}
	likelySafe := []string{}
	keep := []string{}

	now := time.Now()

	for tableName, table := range tables {
		// Phase 1: Zero-usage tables (highest priority)
		if table.ZeroUsage {
			// Apply size filter
			sizeMB := float64(table.TotalBytes) / 1e6
			if sizeMB < config.MinTableSizeMB {
				continue // Skip tables below threshold
			}

			// Score the table (uses special zero-usage scoring)
			score := scorer.Score(table, services)
			table.Score = score

			// Only recommend if score is low enough and not an MV or MV dependency
			if score < 0.30 && !table.IsMV && len(table.MVDependency) == 0 {
				rec := models.TableRecommendation{
					Name:         table.FullName,
					Database:     table.Database,
					Engine:       table.Engine,
					IsReplicated: table.IsReplicated,
					SizeMB:       sizeMB,
					Rows:         table.TotalRows,
				}

				if table.IsReplicated {
					zeroUsageReplicated = append(zeroUsageReplicated, rec)
				} else {
					zeroUsageNonReplicated = append(zeroUsageNonReplicated, rec)
				}
				continue // Don't add to other categories
			}
		}

		// Phase 2: Tables with usage (existing logic)
		// Apply safety rules first
		if !isSafeToRecommend(tableName, table, now) {
			keep = append(keep, tableName)
			continue
		}
		if config.MinQueryCount > 0 && tableQueryCount(table) < config.MinQueryCount {
			likelySafe = append(likelySafe, tableName)
			continue
		}

		// Score the table
		score := scorer.Score(table, services)
		category := scorer.Categorize(score)

		// Update table with score and category
		table.Score = score
		table.Category = category

		// Categorize for recommendations
		switch category {
		case "active":
			keep = append(keep, tableName)
		case "suspect":
			likelySafe = append(likelySafe, tableName)
		case "unused":
			safeToDrop = append(safeToDrop, tableName)
		}
	}

	// Sort zero-usage by size (largest first = highest value cleanup)
	sort.Slice(zeroUsageNonReplicated, func(i, j int) bool {
		return zeroUsageNonReplicated[i].SizeMB > zeroUsageNonReplicated[j].SizeMB
	})
	sort.Slice(zeroUsageReplicated, func(i, j int) bool {
		return zeroUsageReplicated[i].SizeMB > zeroUsageReplicated[j].SizeMB
	})

	if config.Verbose {
		slog.Debug("recommendations summary",
			slog.Int("zero_usage_non_replicated", len(zeroUsageNonReplicated)),
			slog.Int("zero_usage_replicated", len(zeroUsageReplicated)),
			slog.Int("safe_to_drop", len(safeToDrop)),
			slog.Int("likely_safe", len(likelySafe)),
			slog.Int("keep", len(keep)),
		)
	}

	return models.CleanupRecommendations{
		ZeroUsageNonReplicated: zeroUsageNonReplicated,
		ZeroUsageReplicated:    zeroUsageReplicated,
		SafeToDrop:             safeToDrop,
		LikelySafe:             likelySafe,
		Keep:                   keep,
	}
}

// isSafeToRecommend applies safety rules to determine if a table can be recommended for cleanup
func isSafeToRecommend(tableName string, table *models.Table, now time.Time) bool {
	// Rule 1: Never recommend system tables
	if isSystemTable(tableName) {
		return false
	}

	// Rule 2: Never recommend tables with writes in the last 7 days
	daysSinceWrite := now.Sub(table.LastAccess).Hours() / 24
	if table.Writes > 0 && daysSinceWrite < 7 {
		return false
	}

	// Rule 3: Never recommend materialized views (requires special handling)
	if table.IsMV {
		return false
	}

	// Rule 4: Never recommend tables that are MV dependencies
	// (This would require cross-checking with other tables' MVDependency field)
	// For now, we'll be conservative and skip this check

	return true
}

// isSystemTable checks if a table is a system table
func isSystemTable(tableName string) bool {
	lower := strings.ToLower(tableName)
	return strings.HasPrefix(lower, "system.") ||
		strings.HasPrefix(lower, "information_schema.") ||
		strings.HasPrefix(lower, "information_schema.")
}

func tableQueryCount(table *models.Table) uint64 {
	if table == nil {
		return 0
	}

	if len(table.Sparkline) > 0 {
		var total uint64
		for _, point := range table.Sparkline {
			total += point.Value
		}
		return total
	}

	return table.Reads + table.Writes
}

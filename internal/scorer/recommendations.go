package scorer

import (
	"log"
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
	safeToDrop := []string{}
	likelySafe := []string{}
	keep := []string{}

	now := time.Now()

	for tableName, table := range tables {
		// Apply safety rules first
		if !isSafeToRecommend(tableName, table, now) {
			keep = append(keep, tableName)
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

	if config.Verbose {
		log.Printf("Recommendations: %d safe to drop, %d likely safe, %d keep",
			len(safeToDrop), len(likelySafe), len(keep))
	}

	return models.CleanupRecommendations{
		SafeToDrop: safeToDrop,
		LikelySafe: likelySafe,
		Keep:       keep,
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

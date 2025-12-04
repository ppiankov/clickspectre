package scorer

import (
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
)

// SimpleScorer implements a simple scoring algorithm
type SimpleScorer struct{}

// Score calculates a score for a table (0.0 - 1.0)
func (s *SimpleScorer) Score(table *models.Table, services map[string]*models.Service) float64 {
	score := 0.0
	now := time.Now()

	// Factor 1: Recent activity (40% weight)
	daysSinceAccess := now.Sub(table.LastAccess).Hours() / 24
	if daysSinceAccess < 7 {
		score += 0.40
	} else if daysSinceAccess < 30 {
		score += 0.30
	} else if daysSinceAccess < 90 {
		score += 0.10
	}
	// else: no points for very old tables

	// Factor 2: Query volume (30% weight)
	totalQueries := table.Reads + table.Writes
	if totalQueries > 1000 {
		score += 0.30
	} else if totalQueries > 100 {
		score += 0.20
	} else if totalQueries > 10 {
		score += 0.10
	}
	// else: very low activity

	// Factor 3: Access diversity - count unique services using this table (20% weight)
	uniqueServices := countServicesUsingTable(table.FullName, services)
	if uniqueServices > 5 {
		score += 0.20
	} else if uniqueServices > 2 {
		score += 0.15
	} else if uniqueServices > 0 {
		score += 0.05
	}

	// Factor 4: Write activity (10% weight)
	if table.Writes > 0 {
		score += 0.10 // Active writes indicate the table is being maintained
	}

	return score
}

// Categorize returns a category based on the score
func (s *SimpleScorer) Categorize(score float64) string {
	if score >= 0.70 {
		return "active" // Keep
	} else if score >= 0.30 {
		return "suspect" // Likely safe to drop, but needs review
	} else {
		return "unused" // Safe to drop
	}
}

// countServicesUsingTable counts how many services use a given table
func countServicesUsingTable(tableName string, services map[string]*models.Service) int {
	count := 0
	for _, service := range services {
		for _, usedTable := range service.TablesUsed {
			if usedTable == tableName {
				count++
				break
			}
		}
	}
	return count
}

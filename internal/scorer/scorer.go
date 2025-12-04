package scorer

import (
	"github.com/ppiankov/clickspectre/internal/models"
)

// Scorer interface for table scoring algorithms
type Scorer interface {
	Score(table *models.Table, services map[string]*models.Service) float64
	Categorize(score float64) string
}

// NewScorer creates a scorer based on the algorithm name
func NewScorer(algorithm string) Scorer {
	switch algorithm {
	case "simple":
		return &SimpleScorer{}
	default:
		return &SimpleScorer{}
	}
}

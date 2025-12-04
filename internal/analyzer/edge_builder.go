package analyzer

import (
	"log"

	"github.com/ppiankov/clickspectre/internal/models"
)

// EdgeKey uniquely identifies a service→table edge
type EdgeKey struct {
	ServiceIP string
	TableName string
}

// buildEdges creates service→table relationship edges
func (a *Analyzer) buildEdges(entries []*models.QueryLogEntry) error {
	edgeMap := make(map[EdgeKey]*models.Edge)

	for _, entry := range entries {
		clientIP := entry.ClientIP
		if clientIP == "" {
			continue
		}

		// Get service name if available
		serviceName := clientIP
		if service, exists := a.services[clientIP]; exists && service.K8sService != "" {
			serviceName = service.K8sService
		}

		for _, tableName := range entry.Tables {
			if tableName == "" {
				continue
			}

			key := EdgeKey{
				ServiceIP: clientIP,
				TableName: tableName,
			}

			// Get or create edge
			edge, exists := edgeMap[key]
			if !exists {
				edge = &models.Edge{
					ServiceIP:    clientIP,
					ServiceName:  serviceName,
					TableName:    tableName,
					LastActivity: entry.EventTime,
				}
				edgeMap[key] = edge
			}

			// Update edge statistics with actual row counts
			if isReadQuery(entry.QueryKind) {
				edge.Reads += entry.ReadRows
			} else if isWriteQuery(entry.QueryKind) {
				edge.Writes += entry.WrittenRows
			}

			// Update last activity
			if entry.EventTime.After(edge.LastActivity) {
				edge.LastActivity = entry.EventTime
			}
		}
	}

	// Convert map to slice
	a.edges = make([]*models.Edge, 0, len(edgeMap))
	for _, edge := range edgeMap {
		a.edges = append(a.edges, edge)
	}

	if a.config.Verbose {
		log.Printf("Built %d service→table edges", len(a.edges))
	}

	return nil
}

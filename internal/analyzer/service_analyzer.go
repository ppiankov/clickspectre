package analyzer

import (
	"context"
	"log/slog"

	"github.com/ppiankov/clickspectre/internal/models"
)

// buildServiceModel builds the service usage model from query log entries
func (a *Analyzer) buildServiceModel(ctx context.Context, entries []*models.QueryLogEntry) error {
	for _, entry := range entries {
		clientIP := entry.ClientIP
		if clientIP == "" {
			continue
		}

		// Get or create service
		service, exists := a.services[clientIP]
		if !exists {
			service = &models.Service{
				IP:         clientIP,
				TablesUsed: make([]string, 0),
				LastSeen:   entry.EventTime,
			}

			// Resolve K8s service if enabled
			if a.config.ResolveK8s && a.resolver != nil {
				info, err := a.resolver.ResolveIP(ctx, clientIP)
				if err == nil && info != nil {
					service.K8sService = info.Service
					service.K8sNamespace = info.Namespace
					service.K8sPod = info.Pod
				}
			}

			a.services[clientIP] = service
		}

		// Update query count
		service.QueryCount++

		// Update last seen
		if entry.EventTime.After(service.LastSeen) {
			service.LastSeen = entry.EventTime
		}

		// Track tables used by this service
		for _, tableName := range entry.Tables {
			if tableName == "" {
				continue
			}
			if a.config.IsTableExcluded(tableName) {
				continue
			}

			// Add table if not already in list
			found := false
			for _, existing := range service.TablesUsed {
				if existing == tableName {
					found = true
					break
				}
			}
			if !found {
				service.TablesUsed = append(service.TablesUsed, tableName)
			}
		}
	}

	if a.config.Verbose {
		slog.Debug("built service model", slog.Int("services", len(a.services)))
	}

	return nil
}

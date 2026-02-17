package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ppiankov/clickspectre/internal/models"
)

const (
	// DefaultPath is used when --update-baseline is enabled without an explicit --baseline path.
	DefaultPath = ".clickspectre-baseline.json"
	fileVersion = 1
)

// Set stores baseline fingerprints.
type Set map[string]struct{}

// File is the persisted baseline JSON payload.
type File struct {
	Version      int      `json:"version"`
	Fingerprints []string `json:"fingerprints"`
}

// Load reads a baseline file. Missing files return an empty set.
func Load(path string) (Set, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, errors.New("baseline path is empty")
	}

	data, err := os.ReadFile(trimmed)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Set{}, nil
		}
		return nil, fmt.Errorf("read baseline file: %w", err)
	}

	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse baseline file: %w", err)
	}
	if file.Version != 0 && file.Version != fileVersion {
		return nil, fmt.Errorf("unsupported baseline version: %d", file.Version)
	}

	set := Set{}
	for _, fingerprint := range file.Fingerprints {
		if fingerprint == "" {
			continue
		}
		set[fingerprint] = struct{}{}
	}

	return set, nil
}

// Save writes a baseline file with sorted, unique fingerprints.
func Save(path string, set Set) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("baseline path is empty")
	}

	dir := filepath.Dir(trimmed)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create baseline directory: %w", err)
		}
	}

	payload := File{
		Version:      fileVersion,
		Fingerprints: Sorted(set),
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline file: %w", err)
	}

	if err := os.WriteFile(trimmed, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write baseline file: %w", err)
	}

	return nil
}

// AddAll inserts fingerprints into the target set.
func AddAll(target Set, fingerprints []string) {
	for _, fingerprint := range fingerprints {
		if fingerprint == "" {
			continue
		}
		target[fingerprint] = struct{}{}
	}
}

// Sorted returns sorted fingerprints from a set.
func Sorted(set Set) []string {
	fingerprints := make([]string, 0, len(set))
	for fingerprint := range set {
		fingerprints = append(fingerprints, fingerprint)
	}
	sort.Strings(fingerprints)
	return fingerprints
}

// CountFindings returns the number of report items treated as findings.
func CountFindings(report *models.Report) int {
	if report == nil {
		return 0
	}

	recs := report.CleanupRecommendations
	return len(report.Anomalies) +
		len(recs.ZeroUsageNonReplicated) +
		len(recs.ZeroUsageReplicated) +
		len(recs.SafeToDrop) +
		len(recs.LikelySafe)
}

// CollectFingerprints extracts fingerprints for all current findings in the report.
func CollectFingerprints(report *models.Report) []string {
	set := Set{}
	if report == nil {
		return []string{}
	}

	for _, anomaly := range report.Anomalies {
		set[FingerprintAnomaly(anomaly)] = struct{}{}
	}

	for _, rec := range report.CleanupRecommendations.ZeroUsageNonReplicated {
		set[FingerprintTableRecommendation("zero_usage_non_replicated", rec)] = struct{}{}
	}
	for _, rec := range report.CleanupRecommendations.ZeroUsageReplicated {
		set[FingerprintTableRecommendation("zero_usage_replicated", rec)] = struct{}{}
	}
	for _, table := range report.CleanupRecommendations.SafeToDrop {
		set[fingerprintTableName("safe_to_drop", table)] = struct{}{}
	}
	for _, table := range report.CleanupRecommendations.LikelySafe {
		set[fingerprintTableName("likely_safe", table)] = struct{}{}
	}

	return Sorted(set)
}

// SuppressKnown removes findings already present in the baseline set.
func SuppressKnown(report *models.Report, known Set) (suppressed int, remaining int) {
	if report == nil || len(known) == 0 {
		return 0, CountFindings(report)
	}

	report.Anomalies, suppressed = filterAnomalies(report.Anomalies, known, suppressed)
	report.CleanupRecommendations.ZeroUsageNonReplicated, suppressed = filterTableRecommendations(
		report.CleanupRecommendations.ZeroUsageNonReplicated,
		"zero_usage_non_replicated",
		known,
		suppressed,
	)
	report.CleanupRecommendations.ZeroUsageReplicated, suppressed = filterTableRecommendations(
		report.CleanupRecommendations.ZeroUsageReplicated,
		"zero_usage_replicated",
		known,
		suppressed,
	)
	report.CleanupRecommendations.SafeToDrop, suppressed = filterTableNames(
		report.CleanupRecommendations.SafeToDrop,
		"safe_to_drop",
		known,
		suppressed,
	)
	report.CleanupRecommendations.LikelySafe, suppressed = filterTableNames(
		report.CleanupRecommendations.LikelySafe,
		"likely_safe",
		known,
		suppressed,
	)

	return suppressed, CountFindings(report)
}

// FingerprintAnomaly returns a stable fingerprint for an anomaly finding.
func FingerprintAnomaly(anomaly models.Anomaly) string {
	return hash("anomaly", anomaly.Type, anomaly.Severity, anomaly.AffectedTable, anomaly.AffectedService)
}

// FingerprintTableRecommendation returns a stable fingerprint for a recommendation finding.
func FingerprintTableRecommendation(category string, rec models.TableRecommendation) string {
	return hash("recommendation", category, rec.Name, rec.Database)
}

func filterAnomalies(
	anomalies []models.Anomaly,
	known Set,
	suppressed int,
) ([]models.Anomaly, int) {
	filtered := make([]models.Anomaly, 0, len(anomalies))
	for _, anomaly := range anomalies {
		fingerprint := FingerprintAnomaly(anomaly)
		if _, exists := known[fingerprint]; exists {
			suppressed++
			continue
		}
		filtered = append(filtered, anomaly)
	}
	return filtered, suppressed
}

func filterTableRecommendations(
	recommendations []models.TableRecommendation,
	category string,
	known Set,
	suppressed int,
) ([]models.TableRecommendation, int) {
	filtered := make([]models.TableRecommendation, 0, len(recommendations))
	for _, rec := range recommendations {
		fingerprint := FingerprintTableRecommendation(category, rec)
		if _, exists := known[fingerprint]; exists {
			suppressed++
			continue
		}
		filtered = append(filtered, rec)
	}
	return filtered, suppressed
}

func filterTableNames(
	tables []string,
	category string,
	known Set,
	suppressed int,
) ([]string, int) {
	filtered := make([]string, 0, len(tables))
	for _, table := range tables {
		fingerprint := fingerprintTableName(category, table)
		if _, exists := known[fingerprint]; exists {
			suppressed++
			continue
		}
		filtered = append(filtered, table)
	}
	return filtered, suppressed
}

func fingerprintTableName(category string, tableName string) string {
	return hash("recommendation", category, tableName)
}

func hash(parts ...string) string {
	canonical := strings.Join(parts, "\x1f")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

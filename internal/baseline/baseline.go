package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/ppiankov/clickspectre/internal/models" // Corrected import path
)

// DefaultPath is the default file path for the baseline file.
const DefaultPath = ".clickspectre_baseline.json"

// stableFinding represents the fields of a finding that contribute to its unique identity.
// These fields should be stable across different runs and should not include volatile data
// like timestamps, sizes, or metrics.
type StableFinding struct {
	Type            string `json:"type"`                       // e.g., "anomaly", "zero_usage_non_replicated_table"
	Description     string `json:"description,omitempty"`      // Anomaly description
	Severity        string `json:"severity,omitempty"`         // Anomaly severity
	AffectedTable   string `json:"affected_table,omitempty"`   // Anomaly affected table
	AffectedService string `json:"affected_service,omitempty"` // Anomaly affected service
	TableName       string `json:"table_name,omitempty"`       // TableRecommendation name
	DatabaseName    string `json:"database_name,omitempty"`    // TableRecommendation database
	Engine          string `json:"engine,omitempty"`           // TableRecommendation engine
	IsReplicated    bool   `json:"is_replicated,omitempty"`    // TableRecommendation IsReplicated status
}

// Fingerprint generates a SHA-256 hash of the stable fields of the finding.
// This hash serves as a unique identifier for the finding's nature.
func (sf StableFinding) Fingerprint() (string, error) {
	// Use JSON marshaling to ensure a consistent representation for hashing.
	// json.Marshal sorts map keys by default, ensuring consistent output for the same struct.
	b, err := json.Marshal(sf)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:]), nil
}

// finding stores the fingerprint of a stableFinding for easy comparison.
type Finding struct {
	Fingerprint string `json:"fingerprint"`
	Type        string `json:"type"` // Store type for debugging/readability, though Fingerprint is primary key
}

// GenerateFindings converts a models.Report into a slice of stable finding fingerprints.
func GenerateFindings(report *models.Report) ([]Finding, error) {
	var findings []Finding
	var stableFindings []StableFinding

	// Process Anomalies
	for _, a := range report.Anomalies {
		sf := StableFinding{
			Type:            "anomaly",
			Description:     a.Description,
			Severity:        a.Severity,
			AffectedTable:   a.AffectedTable,
			AffectedService: a.AffectedService,
		}
		stableFindings = append(stableFindings, sf)
	}

	// Process CleanupRecommendations
	processTableRecommendations := func(category string, recs []models.TableRecommendation) {
		for _, tr := range recs {
			sf := StableFinding{
				Type:         category, // e.g., "zero_usage_non_replicated_table"
				TableName:    tr.Name,
				DatabaseName: tr.Database,
				Engine:       tr.Engine,
				IsReplicated: tr.IsReplicated,
			}
			stableFindings = append(stableFindings, sf)
		}
	}

	processTableRecommendations("zero_usage_non_replicated_table", report.CleanupRecommendations.ZeroUsageNonReplicated)
	processTableRecommendations("zero_usage_replicated_table", report.CleanupRecommendations.ZeroUsageReplicated)

	// For "safe_to_drop", "likely_safe", "keep" we just have string names.
	// We'll treat these as simpler findings without the full TableRecommendation detail.
	// This ensures we capture the "decision" for each table.
	processStringRecommendations := func(category string, names []string) {
		for _, name := range names {
			// Split full_name (db.table) into database and table
			db, table := SplitTableName(name)
			sf := StableFinding{
				Type:         category,
				TableName:    table,
				DatabaseName: db,
			}
			stableFindings = append(stableFindings, sf)
		}
	}

	processStringRecommendations("safe_to_drop_table", report.CleanupRecommendations.SafeToDrop)
	processStringRecommendations("likely_safe_table", report.CleanupRecommendations.LikelySafe)
	processStringRecommendations("keep_table", report.CleanupRecommendations.Keep)

	// Generate fingerprints for all stable findings
	for _, sf := range stableFindings {
		fp, err := sf.Fingerprint()
		if err != nil {
			return nil, err
		}
		findings = append(findings, Finding{Fingerprint: fp, Type: sf.Type})
	}

	// Sort findings by fingerprint for consistent output, though not strictly necessary for a set comparison.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Fingerprint < findings[j].Fingerprint
	})

	return findings, nil
}

// Load reads baseline findings from a JSON file.
func Load(filePath string) ([]Finding, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Finding{}, nil // Return empty slice if file does not exist
		}
		return nil, err
	}

	var findings []Finding
	if err := json.Unmarshal(data, &findings); err != nil {
		return nil, err
	}
	return findings, nil
}

// Save writes current findings to a JSON file to be used as a baseline.
func Save(filePath string, findings []Finding) error {
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// FilterNewFindings takes a list of current findings and a list of baseline findings,
// returning only those current findings that are not present in the baseline.
func FilterNewFindings(currentFindings, baselineFindings []Finding) []Finding {
	baselineSet := make(map[string]struct{})
	for _, bf := range baselineFindings {
		baselineSet[bf.Fingerprint] = struct{}{}
	}

	var newFindings []Finding
	for _, cf := range currentFindings {
		if _, found := baselineSet[cf.Fingerprint]; !found {
			newFindings = append(newFindings, cf)
		}
	}
	return newFindings
}

// splitTableName splits a "database.table" string into database and table parts.
// It handles cases where the table name might not have a database prefix.
func SplitTableName(fullName string) (string, string) {
	parts := strings.SplitN(fullName, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", fullName // If no dot, assume it's just a table name without a database specified
}

// ApplySuppression modifies the report in-place, removing findings whose fingerprints
// are present in the baselineFindings. It returns the count of suppressed findings.
func ApplySuppression(report *models.Report, baselineFindings []Finding) (int, error) {
	suppressedCount := 0
	baselineSet := make(map[string]struct{})
	for _, bf := range baselineFindings {
		baselineSet[bf.Fingerprint] = struct{}{}
	}

	// Filter Anomalies
	var newAnomalies []models.Anomaly
	for _, a := range report.Anomalies {
		sf := StableFinding{
			Type:            "anomaly",
			Description:     a.Description,
			Severity:        a.Severity,
			AffectedTable:   a.AffectedTable,
			AffectedService: a.AffectedService,
		}
		fp, err := sf.Fingerprint()
		if err != nil {
			return suppressedCount, err
		}
		if _, found := baselineSet[fp]; !found {
			newAnomalies = append(newAnomalies, a)
		} else {
			suppressedCount++
		}
	}
	report.Anomalies = newAnomalies

	// Filter CleanupRecommendations
	filterTableRecommendations := func(category string, recs []models.TableRecommendation) ([]models.TableRecommendation, error) {
		var newRecs []models.TableRecommendation
		for _, tr := range recs {
			sf := StableFinding{
				Type:         category,
				TableName:    tr.Name,
				DatabaseName: tr.Database,
				Engine:       tr.Engine,
				IsReplicated: tr.IsReplicated,
			}
			fp, err := sf.Fingerprint()
			if err != nil {
				return nil, err
			}
			if _, found := baselineSet[fp]; !found {
				newRecs = append(newRecs, tr)
			} else {
				suppressedCount++
			}
		}
		return newRecs, nil
	}

	filterStringRecommendations := func(category string, names []string) ([]string, error) {
		var newNames []string
		for _, name := range names {
			db, table := SplitTableName(name)
			sf := StableFinding{
				Type:         category,
				TableName:    table,
				DatabaseName: db,
			}
			fp, err := sf.Fingerprint()
			if err != nil {
				return nil, err
			}
			if _, found := baselineSet[fp]; !found {
				newNames = append(newNames, name)
			} else {
				suppressedCount++
			}
		}
		return newNames, nil
	}

	var err error
	report.CleanupRecommendations.ZeroUsageNonReplicated, err = filterTableRecommendations("zero_usage_non_replicated_table", report.CleanupRecommendations.ZeroUsageNonReplicated)
	if err != nil {
		return suppressedCount, err
	}
	report.CleanupRecommendations.ZeroUsageReplicated, err = filterTableRecommendations("zero_usage_replicated_table", report.CleanupRecommendations.ZeroUsageReplicated)
	if err != nil {
		return suppressedCount, err
	}
	report.CleanupRecommendations.SafeToDrop, err = filterStringRecommendations("safe_to_drop_table", report.CleanupRecommendations.SafeToDrop)
	if err != nil {
		return suppressedCount, err
	}
	report.CleanupRecommendations.LikelySafe, err = filterStringRecommendations("likely_safe_table", report.CleanupRecommendations.LikelySafe)
	if err != nil {
		return suppressedCount, err
	}
	report.CleanupRecommendations.Keep, err = filterStringRecommendations("keep_table", report.CleanupRecommendations.Keep)
	if err != nil {
		return suppressedCount, err
	}

	return suppressedCount, nil
}

// MergeFindings merges two slices of findings, removing duplicates and returning a new, sorted slice of unique findings.
func MergeFindings(existingFindings, newFindings []Finding) []Finding {
	mergedSet := make(map[string]Finding)
	for _, f := range existingFindings {
		mergedSet[f.Fingerprint] = f
	}
	for _, f := range newFindings {
		mergedSet[f.Fingerprint] = f
	}

	var result []Finding
	for _, f := range mergedSet {
		result = append(result, f)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Fingerprint < result[j].Fingerprint
	})

	return result
}

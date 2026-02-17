package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
)

func TestCollectFingerprintsDeterministic(t *testing.T) {
	reportA := &models.Report{
		Anomalies: []models.Anomaly{
			{
				Type:          "stale_table",
				Severity:      "medium",
				AffectedTable: "db.old_events",
				DetectedAt:    time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC),
			},
		},
		CleanupRecommendations: models.CleanupRecommendations{
			ZeroUsageNonReplicated: []models.TableRecommendation{
				{Name: "db.zero_nonrep", Database: "db", SizeMB: 100, Rows: 10},
			},
			ZeroUsageReplicated: []models.TableRecommendation{
				{Name: "db.zero_rep", Database: "db", IsReplicated: true, SizeMB: 200, Rows: 20},
			},
			SafeToDrop: []string{"db.safe_to_drop"},
			LikelySafe: []string{"db.likely_safe"},
		},
	}

	reportB := &models.Report{
		Anomalies: []models.Anomaly{
			{
				Type:          "stale_table",
				Severity:      "medium",
				AffectedTable: "db.old_events",
				DetectedAt:    time.Date(2026, 2, 18, 10, 0, 0, 0, time.UTC),
			},
		},
		CleanupRecommendations: models.CleanupRecommendations{
			ZeroUsageNonReplicated: []models.TableRecommendation{
				{Name: "db.zero_nonrep", Database: "db", SizeMB: 999, Rows: 999},
			},
			ZeroUsageReplicated: []models.TableRecommendation{
				{Name: "db.zero_rep", Database: "db", IsReplicated: true, SizeMB: 1, Rows: 1},
			},
			SafeToDrop: []string{"db.safe_to_drop"},
			LikelySafe: []string{"db.likely_safe"},
		},
	}

	fingerprintsA := CollectFingerprints(reportA)
	fingerprintsB := CollectFingerprints(reportB)
	if !reflect.DeepEqual(fingerprintsA, fingerprintsB) {
		t.Fatalf("expected deterministic fingerprints, got %v vs %v", fingerprintsA, fingerprintsB)
	}
}

func TestSuppressKnownFiltersReportFindings(t *testing.T) {
	report := &models.Report{
		Anomalies: []models.Anomaly{
			{Type: "stale_table", Severity: "medium", AffectedTable: "db.old_events"},
			{Type: "low_activity", Severity: "low", AffectedTable: "db.rare"},
		},
		CleanupRecommendations: models.CleanupRecommendations{
			ZeroUsageNonReplicated: []models.TableRecommendation{
				{Name: "db.zero_nonrep", Database: "db"},
				{Name: "db.zero_other", Database: "db"},
			},
			ZeroUsageReplicated: []models.TableRecommendation{
				{Name: "db.zero_rep", Database: "db"},
			},
			SafeToDrop: []string{"db.safe_old", "db.safe_new"},
			LikelySafe: []string{"db.likely"},
			Keep:       []string{"db.keep"},
		},
	}

	known := Set{
		FingerprintAnomaly(models.Anomaly{Type: "stale_table", Severity: "medium", AffectedTable: "db.old_events"}):                     {},
		FingerprintTableRecommendation("zero_usage_non_replicated", models.TableRecommendation{Name: "db.zero_nonrep", Database: "db"}): {},
		hash("recommendation", "safe_to_drop", "db.safe_old"):                                                                           {},
	}

	suppressed, remaining := SuppressKnown(report, known)
	if suppressed != 3 {
		t.Fatalf("expected 3 suppressed findings, got %d", suppressed)
	}
	if remaining != 5 {
		t.Fatalf("expected 5 remaining findings, got %d", remaining)
	}

	if len(report.Anomalies) != 1 || report.Anomalies[0].Type != "low_activity" {
		t.Fatalf("unexpected anomalies after suppression: %+v", report.Anomalies)
	}
	if len(report.CleanupRecommendations.ZeroUsageNonReplicated) != 1 ||
		report.CleanupRecommendations.ZeroUsageNonReplicated[0].Name != "db.zero_other" {
		t.Fatalf("unexpected non-replicated recommendations: %+v", report.CleanupRecommendations.ZeroUsageNonReplicated)
	}
	if !reflect.DeepEqual(report.CleanupRecommendations.SafeToDrop, []string{"db.safe_new"}) {
		t.Fatalf("unexpected safe_to_drop: %+v", report.CleanupRecommendations.SafeToDrop)
	}
	if !reflect.DeepEqual(report.CleanupRecommendations.Keep, []string{"db.keep"}) {
		t.Fatalf("expected keep list to remain untouched, got %+v", report.CleanupRecommendations.Keep)
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "baseline.json")

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("expected missing baseline file to be allowed, got %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected empty set for missing baseline, got %d", len(loaded))
	}

	set := Set{
		"b": {},
		"a": {},
	}
	if err := Save(path, set); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err = Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 fingerprints, got %d", len(loaded))
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read baseline file: %v", err)
	}
	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("failed to unmarshal baseline file: %v", err)
	}
	if file.Version != fileVersion {
		t.Fatalf("expected version %d, got %d", fileVersion, file.Version)
	}
	if !reflect.DeepEqual(file.Fingerprints, []string{"a", "b"}) {
		t.Fatalf("expected sorted fingerprints [a b], got %+v", file.Fingerprints)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	payload := `{"version":999,"fingerprints":[]}`
	if err := os.WriteFile(path, []byte(payload), 0644); err != nil {
		t.Fatalf("failed to write baseline file: %v", err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported baseline version") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

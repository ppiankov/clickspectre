package baseline_test

import (
	"github.com/ppiankov/clickspectre/internal/baseline"
	"github.com/ppiankov/clickspectre/internal/models"
	"os"
	"path/filepath"
	"testing"
)

func TestStableFinding_Fingerprint(t *testing.T) {
	sf1 := baseline.StableFinding{
		Type:          "anomaly",
		Description:   "High read activity",
		AffectedTable: "my_db.my_table",
	}
	sf2 := baseline.StableFinding{
		Type:          "anomaly",
		Description:   "High read activity",
		AffectedTable: "my_db.my_table",
	}
	sf3 := baseline.StableFinding{
		Type:          "anomaly",
		Description:   "Low write activity",
		AffectedTable: "my_db.another_table",
	}

	fp1, err := sf1.Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint failed: %v", err)
	}
	fp2, err := sf2.Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint failed: %v", err)
	}
	fp3, err := sf3.Fingerprint()
	if err != nil {
		t.Fatalf("Fingerprint failed: %v", err)
	}

	if fp1 != fp2 {
		t.Errorf("Expected identical fingerprints for identical findings, got %s != %s", fp1, fp2)
	}
	if fp1 == fp3 {
		t.Errorf("Expected different fingerprints for different findings, got %s == %s", fp1, fp3)
	}

	// Test with omitempty fields
	sf4 := baseline.StableFinding{Type: "simple"}
	sf5 := baseline.StableFinding{Type: "simple", Description: ""} // Description is omitempty
	fp4, _ := sf4.Fingerprint()
	fp5, _ := sf5.Fingerprint()
	if fp4 != fp5 {
		t.Errorf("Expected identical fingerprints for omitempty fields, got %s != %s", fp4, fp5)
	}
}

func TestGenerateFindings(t *testing.T) {
	report := &models.Report{
		Anomalies: []models.Anomaly{
			{Type: "HighReads", Description: "Table high reads", AffectedTable: "db.tbl1"},
			{Type: "ZeroWrites", Description: "Table zero writes", AffectedTable: "db.tbl2"},
		},
		CleanupRecommendations: models.CleanupRecommendations{
			ZeroUsageNonReplicated: []models.TableRecommendation{
				{Name: "tbl3", Database: "db", Engine: "MergeTree"},
			},
			SafeToDrop: []string{"db.tbl4"},
			Keep:       []string{"db.tbl5"},
		},
	}

	findings, err := baseline.GenerateFindings(report)
	if err != nil {
		t.Fatalf("GenerateFindings failed: %v", err)
	}

	expectedCount := 5 // 2 anomalies + 1 zero usage + 1 safe to drop + 1 keep
	if len(findings) != expectedCount {
		t.Errorf("Expected %d findings, got %d", expectedCount, len(findings))
	}

	// Check for a specific finding's fingerprint existence
	sfTbl3 := baseline.StableFinding{Type: "zero_usage_non_replicated_table", TableName: "tbl3", DatabaseName: "db", Engine: "MergeTree"}
	fpTbl3, _ := sfTbl3.Fingerprint()
	foundTbl3 := false
	for _, f := range findings {
		if f.Fingerprint == fpTbl3 {
			foundTbl3 = true
			break
		}
	}
	if !foundTbl3 {
		t.Errorf("Expected finding for db.tbl3 not found")
	}

	sfAnomaly := baseline.StableFinding{Type: "anomaly", Description: "Table high reads", AffectedTable: "db.tbl1"}
	fpAnomaly, _ := sfAnomaly.Fingerprint()
	foundAnomaly := false
	for _, f := range findings {
		if f.Fingerprint == fpAnomaly {
			foundAnomaly = true
			break
		}
	}
	if !foundAnomaly {
		t.Errorf("Expected finding for anomaly db.tbl1 not found")
	}
}

func TestLoadAndSave(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "baseline.json")

	initialFindings := []baseline.Finding{
		{Fingerprint: "fp1", Type: "anomaly"},
		{Fingerprint: "fp2", Type: "table_rec"},
	}

	// Test Save
	err := baseline.Save(filePath, initialFindings)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Test Load
	loadedFindings, err := baseline.Load(filePath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(loadedFindings) != len(initialFindings) {
		t.Errorf("Expected %d findings after load, got %d", len(initialFindings), len(loadedFindings))
	}
	if loadedFindings[0].Fingerprint != initialFindings[0].Fingerprint || loadedFindings[1].Fingerprint != initialFindings[1].Fingerprint {
		t.Errorf("Loaded findings mismatch. Expected %+v, got %+v", initialFindings, loadedFindings)
	}

	// Test Load from non-existent file
	nonExistentPath := filepath.Join(dir, "non_existent.json")
	emptyFindings, err := baseline.Load(nonExistentPath)
	if err != nil {
		t.Fatalf("Load from non-existent file failed: %v", err)
	}
	if len(emptyFindings) != 0 {
		t.Errorf("Expected empty findings from non-existent file, got %d", len(emptyFindings))
	}

	// Test malformed JSON
	err = os.WriteFile(filePath, []byte("{malformed"), 0644)
	if err != nil {
		t.Fatalf("Failed to write malformed JSON: %v", err)
	}
	_, err = baseline.Load(filePath)
	if err == nil {
		t.Error("Expected error for malformed JSON, got nil")
	}
}

func TestFilterNewFindings(t *testing.T) {
	f1 := baseline.Finding{Fingerprint: "fp1", Type: "anomaly"}
	f2 := baseline.Finding{Fingerprint: "fp2", Type: "table_rec"}
	f3 := baseline.Finding{Fingerprint: "fp3", Type: "anomaly"}

	current := []baseline.Finding{f1, f2, f3}

	// Case 1: No baseline, all should be new
	newFindings := baseline.FilterNewFindings(current, []baseline.Finding{})
	if len(newFindings) != 3 {
		t.Errorf("Expected 3 new findings with no baseline, got %d", len(newFindings))
	}

	// Case 2: Full baseline, none should be new
	newFindings = baseline.FilterNewFindings(current, []baseline.Finding{f1, f2, f3})
	if len(newFindings) != 0 {
		t.Errorf("Expected 0 new findings with full baseline, got %d", len(newFindings))
	}

	// Case 3: Partial baseline, some should be new
	newFindings = baseline.FilterNewFindings(current, []baseline.Finding{f1})
	if len(newFindings) != 2 {
		t.Errorf("Expected 2 new findings with partial baseline, got %d", len(newFindings))
	}
	if newFindings[0].Fingerprint == "fp1" {
		t.Errorf("Fingerprint fp1 should have been filtered out")
	}
}

func TestApplySuppression(t *testing.T) {
	report := &models.Report{
		Anomalies: []models.Anomaly{
			{Type: "HighReads", Description: "Table high reads", AffectedTable: "db.tbl1"},
			{Type: "ZeroWrites", Description: "Table zero writes", AffectedTable: "db.tbl2"},
		},
		CleanupRecommendations: models.CleanupRecommendations{
			ZeroUsageNonReplicated: []models.TableRecommendation{
				{Name: "tbl3", Database: "db", Engine: "MergeTree"},
			},
			SafeToDrop: []string{"db.tbl4"},
			Keep:       []string{"db.tbl5"},
		},
	}

	// Baseline with tbl1 anomaly and tbl3 zero usage
	sfTbl1Anomaly := baseline.StableFinding{Type: "anomaly", Description: "Table high reads", AffectedTable: "db.tbl1"}
	fpTbl1Anomaly, _ := sfTbl1Anomaly.Fingerprint()

	sfTbl3ZeroUsage := baseline.StableFinding{Type: "zero_usage_non_replicated_table", TableName: "tbl3", DatabaseName: "db", Engine: "MergeTree"}
	fpTbl3ZeroUsage, _ := sfTbl3ZeroUsage.Fingerprint()

	baselineFindings := []baseline.Finding{
		{Fingerprint: fpTbl1Anomaly, Type: "anomaly"},
		{Fingerprint: fpTbl3ZeroUsage, Type: "zero_usage_non_replicated_table"},
	}

	suppressedCount, err := baseline.ApplySuppression(report, baselineFindings)
	if err != nil {
		t.Fatalf("ApplySuppression failed: %v", err)
	}

	if suppressedCount != 2 {
		t.Errorf("Expected 2 suppressed findings, got %d", suppressedCount)
	}

	// Verify anomalies
	if len(report.Anomalies) != 1 {
		t.Errorf("Expected 1 anomaly after suppression, got %d", len(report.Anomalies))
	}
	if report.Anomalies[0].AffectedTable != "db.tbl2" {
		t.Errorf("Expected anomaly for db.tbl2 to remain, but got %s", report.Anomalies[0].AffectedTable)
	}

	// Verify cleanup recommendations
	if len(report.CleanupRecommendations.ZeroUsageNonReplicated) != 0 {
		t.Errorf("Expected 0 zero usage non-replicated tables after suppression, got %d", len(report.CleanupRecommendations.ZeroUsageNonReplicated))
	}
	if len(report.CleanupRecommendations.SafeToDrop) != 1 || report.CleanupRecommendations.SafeToDrop[0] != "db.tbl4" {
		t.Errorf("Expected db.tbl4 to remain in SafeToDrop, got %+v", report.CleanupRecommendations.SafeToDrop)
	}
}

func TestMergeFindings(t *testing.T) {
	f1 := baseline.Finding{Fingerprint: "fp1", Type: "anomaly"}
	f2 := baseline.Finding{Fingerprint: "fp2", Type: "table_rec"}
	f3 := baseline.Finding{Fingerprint: "fp3", Type: "anomaly"}
	f4 := baseline.Finding{Fingerprint: "fp4", Type: "table_rec"}

	existing := []baseline.Finding{f1, f2}
	new := []baseline.Finding{f2, f3, f4} // f2 is a duplicate

	merged := baseline.MergeFindings(existing, new)

	if len(merged) != 4 {
		t.Errorf("Expected 4 unique findings after merge, got %d", len(merged))
	}

	// Verify all fingerprints are present and unique
	expectedFingerprints := map[string]struct{}{"fp1": {}, "fp2": {}, "fp3": {}, "fp4": {}}
	for _, f := range merged {
		if _, ok := expectedFingerprints[f.Fingerprint]; !ok {
			t.Errorf("Unexpected fingerprint in merged list: %s", f.Fingerprint)
		}
		delete(expectedFingerprints, f.Fingerprint)
	}
	if len(expectedFingerprints) != 0 {
		t.Errorf("Missing fingerprints in merged list: %+v", expectedFingerprints)
	}

	// Verify sorting (order should be fp1, fp2, fp3, fp4)
	if merged[0].Fingerprint != "fp1" || merged[1].Fingerprint != "fp2" || merged[2].Fingerprint != "fp3" || merged[3].Fingerprint != "fp4" {
		t.Errorf("Merged findings not sorted correctly: %+v", merged)
	}
}

func TestSplitTableName(t *testing.T) {
	tests := []struct {
		input       string
		expectedDB  string
		expectedTbl string
	}{
		{"mydb.mytable", "mydb", "mytable"},
		{"mytable", "", "mytable"},
		{"", "", ""},
		{".mytable", "", "mytable"}, // Malformed, but should handle gracefully
		{"mydb.", "mydb", ""},       // Malformed, but should handle gracefully
	}

	for _, test := range tests {
		db, tbl := baseline.SplitTableName(test.input)
		if db != test.expectedDB || tbl != test.expectedTbl {
			t.Errorf("For input '%s', expected DB='%s', Tbl='%s'; Got DB='%s', Tbl='%s'",
				test.input, test.expectedDB, test.expectedTbl, db, tbl)
		}
	}
}

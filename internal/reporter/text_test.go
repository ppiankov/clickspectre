package reporter

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func TestWriteTextProducesReadableOutput(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()

	report := &models.Report{
		Timestamp: "2026-02-17T00:00:00Z",
		Metadata: models.Metadata{
			ClickHouseHost:       "clickhouse.internal",
			LookbackDays:         30,
			TotalQueriesAnalyzed: 123,
		},
		Tables: []models.Table{
			{
				Database: "analytics",
				Name:     "events",
				FullName: "analytics.events",
				Score:    0.82,
				Category: "active",
			},
			{
				Database:  "analytics",
				Name:      "old_sessions",
				FullName:  "analytics.old_sessions",
				Score:     0.12,
				Category:  "unused",
				ZeroUsage: true,
			},
		},
		Services: []models.Service{
			{
				IP:           "10.0.0.1",
				K8sNamespace: "prod",
				K8sService:   "api",
			},
		},
		Edges: []models.Edge{
			{
				ServiceIP: "10.0.0.1",
				TableName: "analytics.old_sessions",
				Reads:     5,
				Writes:    1,
			},
		},
		CleanupRecommendations: models.CleanupRecommendations{
			SafeToDrop: []string{"analytics.old_sessions"},
		},
		Anomalies: []models.Anomaly{
			{
				Severity:      "high",
				Description:   "Query spike detected",
				AffectedTable: "analytics.old_sessions",
			},
		},
	}

	var out bytes.Buffer
	if err := writeText(report, cfg, &out); err != nil {
		t.Fatalf("writeText failed: %v", err)
	}

	textOutput := out.String()
	assertContains(t, textOutput, "Summary")
	assertContains(t, textOutput, "Total tables: 2")
	assertContains(t, textOutput, "Unused tables: 1")
	assertContains(t, textOutput, "0.00-0.29: 1")
	assertContains(t, textOutput, "analytics.old_sessions")
	assertContains(t, textOutput, "safety score=0.12")
	assertContains(t, textOutput, "prod/api (reads=5 writes=1)")
	assertContains(t, textOutput, "safe_to_drop")
	assertContains(t, textOutput, "anomaly[high]: Query spike detected")

	if strings.Contains(textOutput, "\x1b[") {
		t.Fatalf("expected no ANSI escape sequences for non-TTY output, got %q", textOutput)
	}

	fileOutput, err := os.ReadFile(filepath.Join(cfg.OutputDir, "report.txt"))
	if err != nil {
		t.Fatalf("failed to read report.txt: %v", err)
	}

	if string(fileOutput) != textOutput {
		t.Fatalf("stdout and report.txt differ\nstdout:\n%s\nfile:\n%s", textOutput, string(fileOutput))
	}
}

func TestWriteTextInputValidation(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	report := &models.Report{}
	var out bytes.Buffer

	err := writeText(nil, cfg, &out)
	if err == nil || !strings.Contains(err.Error(), "report is nil") {
		t.Fatalf("expected nil report error, got %v", err)
	}

	err = writeText(report, nil, &out)
	if err == nil || !strings.Contains(err.Error(), "config is nil") {
		t.Fatalf("expected nil config error, got %v", err)
	}

	err = writeText(report, cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "writer is nil") {
		t.Fatalf("expected nil writer error, got %v", err)
	}
}

func TestReporterGenerateTextFormat(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = t.TempDir()
	cfg.Format = "text"

	rep := New(cfg)

	oldStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stdout pipe: %v", err)
	}
	os.Stdout = writePipe
	t.Cleanup(func() {
		os.Stdout = oldStdout
	})
	t.Cleanup(func() {
		_ = readPipe.Close()
	})
	t.Cleanup(func() {
		_ = writePipe.Close()
	})

	if err := rep.Generate(&models.Report{}); err != nil {
		t.Fatalf("Generate failed for text format: %v", err)
	}

	if err := writePipe.Close(); err != nil {
		t.Fatalf("failed to close write pipe: %v", err)
	}
	if _, err := io.ReadAll(readPipe); err != nil {
		t.Fatalf("failed to read generated text output: %v", err)
	}

	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "report.txt")); err != nil {
		t.Fatalf("expected report.txt output: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.OutputDir, "report.json")); !os.IsNotExist(err) {
		t.Fatalf("expected report.json to be absent for text format, got err=%v", err)
	}
}

func assertContains(t *testing.T, output string, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Fatalf("expected output to contain %q, got:\n%s", want, output)
	}
}

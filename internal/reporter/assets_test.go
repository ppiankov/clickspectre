package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/ppiankov/clickspectre/pkg/config"
)

func setWorkingDir(t *testing.T, dir string) {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	})
}

func createWebFixture(t *testing.T, root string) {
	t.Helper()

	webDir := filepath.Join(root, "web")
	libsDir := filepath.Join(webDir, "libs")
	if err := os.MkdirAll(libsDir, 0755); err != nil {
		t.Fatalf("failed to create fixture directories: %v", err)
	}

	files := map[string]string{
		filepath.Join(webDir, "index.html"):    "<html>ok</html>",
		filepath.Join(webDir, "app.js"):        "console.log('ok')",
		filepath.Join(webDir, "styles.css"):    "body{margin:0}",
		filepath.Join(libsDir, "d3.v7.min.js"): "// d3",
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write fixture file %s: %v", path, err)
		}
	}
}

func TestWriteAssetsSuccess(t *testing.T) {
	root := t.TempDir()
	createWebFixture(t, root)
	setWorkingDir(t, root)

	outDir := filepath.Join(root, "out")
	if err := WriteAssets(outDir); err != nil {
		t.Fatalf("WriteAssets failed: %v", err)
	}

	expected := []string{
		"index.html",
		"app.js",
		"styles.css",
		filepath.Join("libs", "d3.v7.min.js"),
	}
	for _, file := range expected {
		path := filepath.Join(outDir, file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist: %v", path, err)
		}
	}
}

func TestWriteAssetsMissingWebDir(t *testing.T) {
	root := t.TempDir()
	setWorkingDir(t, root)

	err := WriteAssets(filepath.Join(root, "out"))
	if err == nil || !strings.Contains(err.Error(), "web directory not found") {
		t.Fatalf("expected web directory error, got %v", err)
	}
}

func TestWriteAssetsOutputDirError(t *testing.T) {
	root := t.TempDir()
	createWebFixture(t, root)
	setWorkingDir(t, root)

	filePath := filepath.Join(root, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create file path: %v", err)
	}

	err := WriteAssets(filePath)
	if err == nil || !strings.Contains(err.Error(), "failed to create output directory") {
		t.Fatalf("expected output directory creation error, got %v", err)
	}
}

func TestCopyFileError(t *testing.T) {
	err := copyFile(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst"))
	if err == nil {
		t.Fatal("expected copyFile error for missing source")
	}
}

func TestReporterGenerateAndWriteAssets(t *testing.T) {
	root := t.TempDir()
	createWebFixture(t, root)
	setWorkingDir(t, root)

	cfg := config.DefaultConfig()
	cfg.OutputDir = filepath.Join(root, "report")

	rep := New(cfg)
	report := &models.Report{
		Metadata: models.Metadata{
			GeneratedAt:          time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC),
			LookbackDays:         7,
			TotalQueriesAnalyzed: 1,
		},
	}

	if err := rep.Generate(report); err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	files := []string{
		"report.json",
		"index.html",
		"app.js",
		"styles.css",
		filepath.Join("libs", "d3.v7.min.js"),
	}
	for _, file := range files {
		path := filepath.Join(cfg.OutputDir, file)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected generated file %s: %v", path, err)
		}
	}

	if err := rep.WriteAssets(); err != nil {
		t.Fatalf("WriteAssets method failed: %v", err)
	}
}

func TestReporterGenerateWriteJSONError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.OutputDir = filepath.Join(t.TempDir(), "output-file")
	if err := os.WriteFile(cfg.OutputDir, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create output file: %v", err)
	}

	rep := New(cfg)
	err := rep.Generate(&models.Report{})
	if err == nil {
		t.Fatal("expected Generate to fail when output path is a file")
	}
}

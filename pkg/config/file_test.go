package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileParsesWOFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DefaultConfigFileYAML)
	content := `
clickhouse_url: clickhouse://user:pass@ch.internal:9000/default
exclude_tables:
  - analytics.tmp_*
  - old_table
exclude_databases:
  - tmp_*
  - system
min_query_count: 7
format: text
timeout: 10m
min_table_size: 12.5
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if got := cfg.ClickHouseEndpoint(); got != "clickhouse://user:pass@ch.internal:9000/default" {
		t.Fatalf("expected clickhouse endpoint from clickhouse_url, got %q", got)
	}
	if len(cfg.ExcludeTables) != 2 || cfg.ExcludeTables[0] != "analytics.tmp_*" {
		t.Fatalf("unexpected exclude_tables: %v", cfg.ExcludeTables)
	}
	if len(cfg.ExcludeDatabases) != 2 || cfg.ExcludeDatabases[0] != "tmp_*" {
		t.Fatalf("unexpected exclude_databases: %v", cfg.ExcludeDatabases)
	}
	if cfg.MinQueryCount == nil || *cfg.MinQueryCount != 7 {
		t.Fatalf("expected min_query_count=7, got %v", cfg.MinQueryCount)
	}
	if cfg.MinTableSizeMB == nil || *cfg.MinTableSizeMB != 12.5 {
		t.Fatalf("expected min_table_size=12.5, got %v", cfg.MinTableSizeMB)
	}
	if got := cfg.Format; got != "text" {
		t.Fatalf("expected format=text, got %q", got)
	}
	if got := cfg.QueryTimeoutValue(); got != "10m" {
		t.Fatalf("expected timeout=10m, got %q", got)
	}
}

func TestAutoLoadFilePrefersCWD(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()

	cwdFile := filepath.Join(cwd, DefaultConfigFileYAML)
	homeFile := filepath.Join(home, DefaultConfigFileYAML)

	if err := os.WriteFile(cwdFile, []byte("clickhouse_url: clickhouse://cwd:9000/default\n"), 0o644); err != nil {
		t.Fatalf("failed to write cwd config file: %v", err)
	}
	if err := os.WriteFile(homeFile, []byte("clickhouse_url: clickhouse://home:9000/default\n"), 0o644); err != nil {
		t.Fatalf("failed to write home config file: %v", err)
	}

	t.Setenv("HOME", home)
	t.Chdir(cwd)

	cfg, path, err := AutoLoadFile()
	if err != nil {
		t.Fatalf("AutoLoadFile failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config file to be loaded")
	}
	if got := cfg.ClickHouseEndpoint(); got != "clickhouse://cwd:9000/default" {
		t.Fatalf("expected cwd config to win, got %q", got)
	}
	if path != DefaultConfigFileYAML {
		t.Fatalf("expected returned path to be %q, got %q", DefaultConfigFileYAML, path)
	}
}

func TestLoadFirstExistingFileNoMatch(t *testing.T) {
	cfg, path, err := LoadFirstExistingFile([]string{
		filepath.Join(t.TempDir(), "missing-1.yaml"),
		filepath.Join(t.TempDir(), "missing-2.yaml"),
	})
	if err != nil {
		t.Fatalf("expected no error when no files found, got %v", err)
	}
	if cfg != nil || path != "" {
		t.Fatalf("expected nil config and empty path, got cfg=%v path=%q", cfg, path)
	}
}

func TestExcludePatternMatching(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExcludeDatabases = []string{"tmp_*", "internal"}
	cfg.ExcludeTables = []string{"analytics.ignore_*", "legacy_table"}
	cfg.Normalize()

	if !cfg.IsDatabaseExcluded("TMP_DB") {
		t.Fatal("expected tmp_db to match tmp_* database exclusion")
	}
	if !cfg.IsTableExcluded("tmp_db.events") {
		t.Fatal("expected table in excluded database to be excluded")
	}
	if !cfg.IsTableExcluded("analytics.ignore_me") {
		t.Fatal("expected analytics.ignore_me to match table pattern")
	}
	if !cfg.IsTableExcluded("analytics.legacy_table") {
		t.Fatal("expected bare table name pattern to match")
	}
	if cfg.IsTableExcluded("analytics.events") {
		t.Fatal("did not expect analytics.events to be excluded")
	}
}

func TestFileConfigTimeoutFallback(t *testing.T) {
	cfg := &FileConfig{
		QueryTimeout: "20m",
	}
	if got := cfg.QueryTimeoutValue(); got != "20m" {
		t.Fatalf("expected fallback to query_timeout, got %q", got)
	}
}

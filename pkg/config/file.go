package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigFileYAML is the canonical config filename.
	DefaultConfigFileYAML = ".clickspectre.yaml"
	// DefaultConfigFileYML is a compatible alternate config filename.
	DefaultConfigFileYML = ".clickspectre.yml"
)

// FileConfig represents values loaded from a .clickspectre.yaml file.
type FileConfig struct {
	ClickHouseURL    string   `yaml:"clickhouse_url"`
	ClickHouseDSN    string   `yaml:"clickhouse_dsn"`
	ExcludeTables    []string `yaml:"exclude_tables"`
	ExcludeDatabases []string `yaml:"exclude_databases"`
	MinQueryCount    *uint64  `yaml:"min_query_count"`
	Format           string   `yaml:"format"`
	Timeout          string   `yaml:"timeout"`
	QueryTimeout     string   `yaml:"query_timeout"`
	MinTableSizeMB   *float64 `yaml:"min_table_size"`
}

// ClickHouseEndpoint returns the first configured ClickHouse endpoint.
func (fc *FileConfig) ClickHouseEndpoint() string {
	if fc == nil {
		return ""
	}
	if dsn := strings.TrimSpace(fc.ClickHouseDSN); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(fc.ClickHouseURL)
}

// QueryTimeoutValue returns timeout from timeout/query_timeout fields.
func (fc *FileConfig) QueryTimeoutValue() string {
	if fc == nil {
		return ""
	}
	if timeout := strings.TrimSpace(fc.Timeout); timeout != "" {
		return timeout
	}
	return strings.TrimSpace(fc.QueryTimeout)
}

// Normalize trims and removes empty items from list fields.
func (fc *FileConfig) Normalize() {
	if fc == nil {
		return
	}
	fc.ExcludeTables = normalizeList(fc.ExcludeTables)
	fc.ExcludeDatabases = normalizeList(fc.ExcludeDatabases)
	fc.ClickHouseURL = strings.TrimSpace(fc.ClickHouseURL)
	fc.ClickHouseDSN = strings.TrimSpace(fc.ClickHouseDSN)
	fc.Format = strings.TrimSpace(fc.Format)
	fc.Timeout = strings.TrimSpace(fc.Timeout)
	fc.QueryTimeout = strings.TrimSpace(fc.QueryTimeout)
}

// AutoLoadFile discovers and loads the first available config file.
func AutoLoadFile() (*FileConfig, string, error) {
	candidates := []string{
		DefaultConfigFileYAML,
		DefaultConfigFileYML,
	}

	if homeDir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		candidates = append(candidates,
			filepath.Join(homeDir, DefaultConfigFileYAML),
			filepath.Join(homeDir, DefaultConfigFileYML),
		)
	}

	return LoadFirstExistingFile(candidates)
}

// LoadFirstExistingFile loads the first config file that exists in paths.
func LoadFirstExistingFile(paths []string) (*FileConfig, string, error) {
	for _, path := range paths {
		candidate := strings.TrimSpace(path)
		if candidate == "" {
			continue
		}

		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, "", fmt.Errorf("failed to access config file %q: %w", candidate, err)
		}
		if info.IsDir() {
			return nil, "", fmt.Errorf("config path %q is a directory, expected a file", candidate)
		}

		cfg, err := LoadFile(candidate)
		if err != nil {
			return nil, "", err
		}
		return cfg, candidate, nil
	}

	return nil, "", nil
}

// LoadFile loads config values from a specific YAML file path.
func LoadFile(path string) (*FileConfig, error) {
	filename := strings.TrimSpace(path)
	if filename == "" {
		return nil, fmt.Errorf("config path is empty")
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", filename, err)
	}

	cfg := &FileConfig{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %w", filename, err)
	}

	cfg.Normalize()
	return cfg, nil
}

func normalizeList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

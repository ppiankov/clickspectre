package config

import (
	"path"
	"strings"
)

// Normalize trims config patterns and removes empty values.
func (c *Config) Normalize() {
	if c == nil {
		return
	}
	c.ExcludeTables = normalizePatterns(c.ExcludeTables)
	c.ExcludeDatabases = normalizePatterns(c.ExcludeDatabases)
}

// IsDatabaseExcluded reports whether database matches exclude patterns.
func (c *Config) IsDatabaseExcluded(database string) bool {
	if c == nil || len(c.ExcludeDatabases) == 0 {
		return false
	}

	value := normalizePattern(database)
	if value == "" {
		return false
	}

	for _, pattern := range c.ExcludeDatabases {
		if patternMatches(pattern, value) {
			return true
		}
	}

	return false
}

// IsTableExcluded reports whether table matches exclude tables/databases patterns.
func (c *Config) IsTableExcluded(fullName string) bool {
	if c == nil {
		return false
	}

	normalized := normalizePattern(fullName)
	if normalized == "" {
		return false
	}

	database, table := splitTableName(normalized)
	if database != "" && c.IsDatabaseExcluded(database) {
		return true
	}

	if len(c.ExcludeTables) == 0 {
		return false
	}

	for _, pattern := range c.ExcludeTables {
		if patternMatches(pattern, normalized) {
			return true
		}
		if table != "" && patternMatches(pattern, table) {
			return true
		}
	}

	return false
}

func splitTableName(fullName string) (database string, table string) {
	parts := strings.SplitN(fullName, ".", 2)
	if len(parts) < 2 {
		return "", strings.TrimSpace(fullName)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func normalizePatterns(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	normalized := make([]string, 0, len(values))
	for _, pattern := range values {
		p := normalizePattern(pattern)
		if p == "" {
			continue
		}
		normalized = append(normalized, p)
	}
	return normalized
}

func normalizePattern(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func patternMatches(pattern, value string) bool {
	normalizedPattern := normalizePattern(pattern)
	normalizedValue := normalizePattern(value)
	if normalizedPattern == "" || normalizedValue == "" {
		return false
	}

	// Invalid glob patterns are treated as exact matches.
	matched, err := path.Match(normalizedPattern, normalizedValue)
	if err == nil {
		return matched
	}
	return normalizedPattern == normalizedValue
}

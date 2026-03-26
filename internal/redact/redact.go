package redact

import (
	"regexp"
	"strings"
)

// Patterns for common secrets in SQL queries.
var patterns = []*regexp.Regexp{
	// password='...' or password = '...'
	regexp.MustCompile(`(?i)(password|passwd|pwd)\s*=\s*'[^']*'`),
	// token='...' or token = '...'
	regexp.MustCompile(`(?i)(token|secret|api_key|apikey|access_key|secret_key)\s*=\s*'[^']*'`),
	// DSN strings with credentials
	regexp.MustCompile(`(clickhouse|postgres|postgresql|mysql|mongodb|redis)://[^\s'"]+:[^\s'"@]+@`),
	// Bearer tokens
	regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9\-._~+/]+=*`),
	// Long hex strings that look like keys (32+ chars)
	regexp.MustCompile(`(?i)(key|token|secret)\s*=\s*'[0-9a-f]{32,}'`),
}

// replacements for each pattern — redact the sensitive part only.
var replacements = []string{
	"$1=[REDACTED]",
	"$1=[REDACTED]",
	"$1://[REDACTED]@",
	"Bearer [REDACTED]",
	"$1=[REDACTED]",
}

// Query redacts potential secrets from a SQL query string.
// Returns the redacted query and whether any redaction was applied.
func Query(q string) (string, bool) {
	original := q
	for i, p := range patterns {
		q = p.ReplaceAllString(q, replacements[i])
	}
	return q, q != original
}

// ContainsSecrets checks if a query string likely contains secrets.
func ContainsSecrets(q string) bool {
	lower := strings.ToLower(q)
	for _, keyword := range []string{"password", "token", "secret", "api_key", "bearer"} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	// Check for DSN-like patterns
	for _, scheme := range []string{"clickhouse://", "postgres://", "mysql://", "mongodb://"} {
		if strings.Contains(lower, scheme) {
			return true
		}
	}
	return false
}

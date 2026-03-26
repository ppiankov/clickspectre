package redact

import (
	"strings"
	"testing"
)

func TestQueryRedactsPasswords(t *testing.T) {
	input := "INSERT INTO users VALUES ('admin', password='s3cret123')"
	result, changed := Query(input)
	if !changed {
		t.Fatal("expected redaction")
	}
	if strings.Contains(result, "s3cret123") {
		t.Fatalf("password not redacted: %s", result)
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker: %s", result)
	}
}

func TestQueryRedactsDSN(t *testing.T) {
	input := "SELECT * FROM remote('clickhouse://user:pass@host:9000/db', 'table')"
	result, changed := Query(input)
	if !changed {
		t.Fatal("expected redaction")
	}
	if strings.Contains(result, "pass") {
		t.Fatalf("DSN password not redacted: %s", result)
	}
}

func TestQueryRedactsTokens(t *testing.T) {
	input := "WHERE token='abc123def456'"
	result, changed := Query(input)
	if !changed {
		t.Fatal("expected redaction")
	}
	if strings.Contains(result, "abc123def456") {
		t.Fatalf("token not redacted: %s", result)
	}
}

func TestQueryCleanPassthrough(t *testing.T) {
	input := "SELECT count() FROM events WHERE date > '2026-01-01'"
	result, changed := Query(input)
	if changed {
		t.Fatalf("expected no redaction for clean query, got: %s", result)
	}
	if result != input {
		t.Fatalf("clean query modified: %s", result)
	}
}

func TestContainsSecrets(t *testing.T) {
	tests := []struct {
		q    string
		want bool
	}{
		{"SELECT 1", false},
		{"password='x'", true},
		{"clickhouse://user:pass@host", true},
		{"Bearer abc123", true},
		{"WHERE api_key = 'x'", true},
		{"SELECT count() FROM events", false},
	}
	for _, tc := range tests {
		got := ContainsSecrets(tc.q)
		if got != tc.want {
			t.Errorf("ContainsSecrets(%q) = %v, want %v", tc.q, got, tc.want)
		}
	}
}

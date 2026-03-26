// Package policy implements policy-as-code for ClickHouse table hygiene rules.
package policy

import (
	"fmt"
	"os"

	"github.com/ppiankov/clickspectre/internal/models"
	"gopkg.in/yaml.v3"
)

// Policy defines table hygiene rules.
type Policy struct {
	MaxZeroUsageDays    int     `yaml:"max_zero_usage_days"`
	RequireReplication  bool    `yaml:"require_replication"`
	MaxTableSizeGB      float64 `yaml:"max_table_size_gb"`
	MinQueryCountPer30d int64   `yaml:"min_query_count_per_30d"`
}

// Violation represents a policy violation finding.
type Violation struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Table    string `json:"table"`
	Message  string `json:"message"`
}

// DefaultPolicy returns sensible defaults.
func DefaultPolicy() *Policy {
	return &Policy{
		MaxZeroUsageDays:    90,
		RequireReplication:  false,
		MaxTableSizeGB:      0, // disabled
		MinQueryCountPer30d: 0, // disabled
	}
}

// Load reads a policy from a YAML file.
func Load(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}

	p := DefaultPolicy()
	if err := yaml.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("parse policy file: %w", err)
	}
	return p, nil
}

// Evaluate checks a report against the policy and returns violations.
func (p *Policy) Evaluate(report *models.Report) []Violation {
	var violations []Violation

	for _, t := range report.Tables {
		// Rule: zero-usage tables exceeding max age
		if p.MaxZeroUsageDays > 0 && t.ZeroUsage {
			violations = append(violations, Violation{
				Rule:     "max_zero_usage_days",
				Severity: "high",
				Table:    t.FullName,
				Message:  fmt.Sprintf("table %q has zero usage (max allowed: %d days)", t.FullName, p.MaxZeroUsageDays),
			})
		}

		// Rule: require replication
		if p.RequireReplication && !t.IsReplicated && !t.ZeroUsage {
			violations = append(violations, Violation{
				Rule:     "require_replication",
				Severity: "medium",
				Table:    t.FullName,
				Message:  fmt.Sprintf("active table %q is not replicated (engine: %s)", t.FullName, t.Engine),
			})
		}

		// Rule: max table size
		if p.MaxTableSizeGB > 0 {
			sizeGB := float64(t.TotalBytes) / (1 << 30)
			if sizeGB > p.MaxTableSizeGB {
				violations = append(violations, Violation{
					Rule:     "max_table_size_gb",
					Severity: "medium",
					Table:    t.FullName,
					Message:  fmt.Sprintf("table %q is %.1f GB (max: %.1f GB)", t.FullName, sizeGB, p.MaxTableSizeGB),
				})
			}
		}

		// Rule: minimum query count
		if p.MinQueryCountPer30d > 0 && !t.ZeroUsage {
			totalQueries := int64(t.Reads + t.Writes)
			if totalQueries < p.MinQueryCountPer30d {
				violations = append(violations, Violation{
					Rule:     "min_query_count_per_30d",
					Severity: "low",
					Table:    t.FullName,
					Message:  fmt.Sprintf("table %q has %d queries (min required: %d)", t.FullName, totalQueries, p.MinQueryCountPer30d),
				})
			}
		}
	}

	return violations
}

// Template returns a commented YAML policy template.
func Template() string {
	return `# clickspectre policy — table hygiene rules
# Use with: clickspectre analyze --policy .clickspectre-policy.yaml

# Maximum days a table can have zero queries before violation (0 = disabled)
max_zero_usage_days: 90

# Require all active tables to use a replicated engine
require_replication: false

# Maximum table size in GB (0 = disabled)
# max_table_size_gb: 500

# Minimum query count per 30-day period for active tables (0 = disabled)
# min_query_count_per_30d: 10
`
}

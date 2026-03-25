package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const defaultConfigTemplate = `# clickspectre configuration
# See: https://github.com/ppiankov/clickspectre

# ClickHouse connection
# clickhouse_dsn: "clickhouse://user:password@host:9000/default"

# Output format: json, text, sarif, spectrehub
# format: json

# Query timeout (e.g., 5m, 10m, 1h)
# query_timeout: "5m"

# Exclude tables by glob pattern
# exclude_tables:
#   - "system.*"
#   - "*.tmp_*"

# Exclude databases by glob pattern
# exclude_databases:
#   - "system"

# Minimum query count to consider a table active
# min_query_count: 0

# Minimum table size in MB for unused table recommendations
# min_table_size: 1.0
`

// NewInitCmd creates the init command
func NewInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a .clickspectre.yaml config file with defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			const configFile = ".clickspectre.yaml"

			if !force {
				if _, err := os.Stat(configFile); err == nil {
					return fmt.Errorf("%s already exists (use --force to overwrite)", configFile)
				}
			}

			if err := os.WriteFile(configFile, []byte(defaultConfigTemplate), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", configFile, err)
			}

			cmd.Printf("Created %s\n", configFile)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config file")

	return cmd
}

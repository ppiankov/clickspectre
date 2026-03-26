package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ppiankov/clickspectre/internal/collector"
	"github.com/ppiankov/clickspectre/internal/logging"
	"github.com/ppiankov/clickspectre/pkg/config"
	"github.com/spf13/cobra"
)

// GrantUser represents a CH user with their grants.
type GrantUser struct {
	Name       string   `json:"name"`
	AuthType   string   `json:"auth_type"`
	Grants     []string `json:"grants"`
	QueryCount int64    `json:"query_count,omitempty"`
	LastSeen   string   `json:"last_seen,omitempty"`
	IsActive   bool     `json:"is_active"`
}

// GrantsOutput is the structured output.
type GrantsOutput struct {
	Users []GrantUser `json:"users"`
}

// NewGrantsCmd creates the grants command.
func NewGrantsCmd() *cobra.Command {
	var (
		dsn      string
		unused   bool
		lookback string
		format   string
	)

	cmd := &cobra.Command{
		Use:   "grants [user]",
		Short: "Show user permissions — access lifecycle audit",
		Long:  "List ClickHouse users with their grants. Use --unused to find users with permissions but zero queries (revoke candidates).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var logOpts []logging.Option
			if quiet {
				logOpts = append(logOpts, logging.WithQuiet())
			}
			logging.Init(verbose, logOpts...)

			if dsn == "" {
				return fmt.Errorf("required flag --clickhouse-dsn not set")
			}

			cfg := config.DefaultConfig()
			cfg.ClickHouseDSN = dsn
			cfg.ClickHouseDSNs = strings.Split(dsn, ",")
			for i := range cfg.ClickHouseDSNs {
				cfg.ClickHouseDSNs[i] = strings.TrimSpace(cfg.ClickHouseDSNs[i])
			}

			col, err := collector.New(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect: %w", err)
			}
			defer func() { _ = col.Close() }()

			var filterUser string
			if len(args) > 0 {
				filterUser = args[0]
			}

			var lookbackDur time.Duration
			if unused {
				lookbackDur, err = config.ParseDuration(lookback)
				if err != nil {
					return fmt.Errorf("invalid --lookback: %w", err)
				}
			}

			output, err := runGrants(col, filterUser, unused, lookbackDur)
			if err != nil {
				return err
			}

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(output)
			}

			printGrants(cmd, output, unused)
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN")
	cmd.Flags().BoolVar(&unused, "unused", false, "Show only users with grants but zero queries")
	cmd.Flags().StringVar(&lookback, "lookback", "30d", "Lookback period for --unused")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")

	return cmd
}

func runGrants(col collector.Collector, filterUser string, unused bool, lookback time.Duration) (*GrantsOutput, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Get users
	userQuery := "SELECT name, auth_type FROM system.users ORDER BY name"
	if filterUser != "" {
		userQuery = "SELECT name, auth_type FROM system.users WHERE name = ?"
	}

	var userArgs []interface{}
	if filterUser != "" {
		userArgs = append(userArgs, filterUser)
	}

	rows, err := col.QueryRaw(ctx, userQuery, userArgs...)
	if err != nil {
		return nil, fmt.Errorf("query users failed: %w", err)
	}

	var users []GrantUser
	for rows.Next() {
		var u GrantUser
		if err := rows.Scan(&u.Name, &u.AuthType); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan user failed: %w", err)
		}
		users = append(users, u)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("users rows error: %w", err)
	}

	// Get grants per user
	for i := range users {
		grants, err := fetchUserGrants(ctx, col, users[i].Name)
		if err != nil {
			return nil, err
		}
		users[i].Grants = grants
	}

	// Cross-reference with query_log if --unused
	if unused && lookback > 0 {
		activity, err := fetchUserActivity(ctx, col, lookback)
		if err != nil {
			return nil, err
		}
		for i := range users {
			if act, ok := activity[users[i].Name]; ok {
				users[i].QueryCount = act.count
				users[i].LastSeen = act.lastSeen.Format("2006-01-02")
				users[i].IsActive = true
			}
		}

		// Filter to unused only
		var filtered []GrantUser
		for _, u := range users {
			if !u.IsActive {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	} else {
		// Mark all as active (we don't know without query_log check)
		for i := range users {
			users[i].IsActive = true
		}
	}

	return &GrantsOutput{Users: users}, nil
}

func fetchUserGrants(ctx context.Context, col collector.Collector, username string) ([]string, error) {
	rows, err := col.QueryRaw(ctx,
		"SELECT concat(access_type, ' ON ', database, '.', `table`) FROM system.grants WHERE user_name = ? ORDER BY access_type",
		username)
	if err != nil {
		return nil, fmt.Errorf("query grants for %s failed: %w", username, err)
	}
	defer func() { _ = rows.Close() }()

	var grants []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, fmt.Errorf("scan grant failed: %w", err)
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

type userAct struct {
	count    int64
	lastSeen time.Time
}

func fetchUserActivity(ctx context.Context, col collector.Collector, lookback time.Duration) (map[string]userAct, error) {
	seconds := int(lookback.Seconds())
	rows, err := col.QueryRaw(ctx, `
		SELECT user, count() AS cnt, max(event_time) AS last_seen
		FROM system.query_log
		WHERE event_time >= now() - INTERVAL ? SECOND
		  AND type = 'QueryFinish'
		GROUP BY user
	`, seconds)
	if err != nil {
		return nil, fmt.Errorf("query activity failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]userAct)
	for rows.Next() {
		var name string
		var a userAct
		if err := rows.Scan(&name, &a.count, &a.lastSeen); err != nil {
			return nil, fmt.Errorf("scan activity failed: %w", err)
		}
		result[name] = a
	}
	return result, rows.Err()
}

func printGrants(cmd *cobra.Command, output *GrantsOutput, showUnused bool) {
	if len(output.Users) == 0 {
		if showUnused {
			cmd.Println("No unused users found.")
		} else {
			cmd.Println("No users found.")
		}
		return
	}

	if showUnused {
		cmd.Printf("Unused users (grants but no queries):\n\n")
	}

	for _, u := range output.Users {
		cmd.Printf("%s (auth: %s)\n", u.Name, u.AuthType)
		if len(u.Grants) == 0 {
			cmd.Println("  (no explicit grants)")
		}
		for _, g := range u.Grants {
			cmd.Printf("  %s\n", g)
		}
		if u.QueryCount > 0 {
			cmd.Printf("  queries: %d, last seen: %s\n", u.QueryCount, u.LastSeen)
		}
		cmd.Println()
	}
	cmd.Printf("%d user(s)\n", len(output.Users))
}

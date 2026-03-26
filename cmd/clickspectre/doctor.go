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

// DoctorCheck is a single diagnostic result.
type DoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fail, skip
	Detail string `json:"detail,omitempty"`
}

// DoctorOutput is the structured output of the doctor command.
type DoctorOutput struct {
	Checks  []DoctorCheck `json:"checks"`
	Passed  int           `json:"passed"`
	Failed  int           `json:"failed"`
	Skipped int           `json:"skipped"`
}

// NewDoctorCmd creates the doctor command.
func NewDoctorCmd() *cobra.Command {
	var (
		dsn        string
		configPath string
		format     string
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check connectivity and configuration health",
		Long:  "Runs diagnostic checks against ClickHouse, config files, and local state. Agents use this to verify prerequisites before running analysis.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var logOpts []logging.Option
			if quiet {
				logOpts = append(logOpts, logging.WithQuiet())
			}
			logging.Init(verbose, logOpts...)

			output := runDoctor(dsn, configPath)

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(output)
			}

			// Text output
			for _, c := range output.Checks {
				var icon string
				switch c.Status {
				case "fail":
					icon = "✗"
				case "skip":
					icon = "-"
				default:
					icon = "✓"
				}
				if c.Detail != "" {
					cmd.Printf("  %s %s: %s\n", icon, c.Name, c.Detail)
				} else {
					cmd.Printf("  %s %s\n", icon, c.Name)
				}
			}
			cmd.Printf("\n%d passed, %d failed, %d skipped\n",
				output.Passed, output.Failed, output.Skipped)

			if output.Failed > 0 {
				return fmt.Errorf("%d check(s) failed", output.Failed)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "clickhouse-dsn", "", "ClickHouse DSN")
	cmd.Flags().StringVar(&configPath, "config", "", "Config file path")
	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")

	return cmd
}

func runDoctor(dsn, configPath string) *DoctorOutput {
	output := &DoctorOutput{}

	// 1. Config file check
	output.addCheck(checkConfigFile(configPath))

	// 2. DSN resolution
	resolvedDSN := dsn
	if resolvedDSN == "" {
		// Try loading from config
		if fc, _, err := config.AutoLoadFile(); err == nil && fc != nil {
			if ep := fc.ClickHouseEndpoint(); ep != "" {
				resolvedDSN = ep
			}
		}
	}

	if resolvedDSN == "" {
		output.addCheck(DoctorCheck{
			Name:   "ClickHouse DSN",
			Status: "fail",
			Detail: "no DSN provided via --clickhouse-dsn or config file",
		})
		// Can't check connectivity without a DSN
		output.addCheck(DoctorCheck{Name: "ClickHouse connectivity", Status: "skip", Detail: "no DSN"})
		output.addCheck(DoctorCheck{Name: "system.query_log", Status: "skip", Detail: "no DSN"})
	} else {
		output.addCheck(DoctorCheck{
			Name:   "ClickHouse DSN",
			Status: "pass",
			Detail: maskDSN(resolvedDSN),
		})

		// 3. Connectivity check
		cfg := config.DefaultConfig()
		cfg.ClickHouseDSN = resolvedDSN
		cfg.ClickHouseDSNs = strings.Split(resolvedDSN, ",")
		for i := range cfg.ClickHouseDSNs {
			cfg.ClickHouseDSNs[i] = strings.TrimSpace(cfg.ClickHouseDSNs[i])
		}

		col, err := collector.New(cfg)
		if err != nil {
			output.addCheck(DoctorCheck{
				Name:   "ClickHouse connectivity",
				Status: "fail",
				Detail: err.Error(),
			})
			output.addCheck(DoctorCheck{Name: "system.query_log", Status: "skip", Detail: "not connected"})
		} else {
			defer func() { _ = col.Close() }()
			output.addCheck(DoctorCheck{
				Name:   "ClickHouse connectivity",
				Status: "pass",
			})

			// 4. query_log check
			output.addCheck(checkQueryLog(col))
		}
	}

	// 5. Watermark file
	output.addCheck(checkWatermark())

	return output
}

func (o *DoctorOutput) addCheck(c DoctorCheck) {
	o.Checks = append(o.Checks, c)
	switch c.Status {
	case "pass":
		o.Passed++
	case "fail":
		o.Failed++
	case "skip":
		o.Skipped++
	}
}

func checkConfigFile(path string) DoctorCheck {
	if path != "" {
		if _, err := config.LoadFile(path); err != nil {
			return DoctorCheck{Name: "Config file", Status: "fail", Detail: err.Error()}
		}
		return DoctorCheck{Name: "Config file", Status: "pass", Detail: path}
	}

	fc, foundPath, err := config.AutoLoadFile()
	if err != nil {
		return DoctorCheck{Name: "Config file", Status: "fail", Detail: err.Error()}
	}
	if fc == nil {
		return DoctorCheck{Name: "Config file", Status: "skip", Detail: "no .clickspectre.yaml found (optional)"}
	}
	return DoctorCheck{Name: "Config file", Status: "pass", Detail: foundPath}
}

func checkQueryLog(col collector.Collector) DoctorCheck {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rows, err := col.QueryRaw(ctx, "SELECT count() FROM system.query_log WHERE event_time >= now() - INTERVAL 1 DAY AND type = 'QueryFinish'")
	if err != nil {
		return DoctorCheck{Name: "system.query_log", Status: "fail", Detail: err.Error()}
	}
	defer func() { _ = rows.Close() }()

	var count int64
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return DoctorCheck{Name: "system.query_log", Status: "fail", Detail: err.Error()}
		}
	}

	if count == 0 {
		return DoctorCheck{Name: "system.query_log", Status: "pass", Detail: "accessible but empty (no queries in last 24h)"}
	}
	return DoctorCheck{
		Name:   "system.query_log",
		Status: "pass",
		Detail: fmt.Sprintf("%d entries in last 24h", count),
	}
}

func checkWatermark() DoctorCheck {
	path := collector.DefaultWatermarkPath()
	wm, err := collector.LoadWatermark(path)
	if err != nil {
		return DoctorCheck{Name: "Watermark file", Status: "fail", Detail: err.Error()}
	}
	if wm == nil {
		return DoctorCheck{Name: "Watermark file", Status: "skip", Detail: "not found (created on first --incremental run)"}
	}

	age := time.Since(wm.LastRun).Round(time.Minute)
	return DoctorCheck{
		Name:   "Watermark file",
		Status: "pass",
		Detail: fmt.Sprintf("last run %s ago", age),
	}
}

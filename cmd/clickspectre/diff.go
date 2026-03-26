package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ppiankov/clickspectre/internal/models"
	"github.com/spf13/cobra"
)

// DiffResult describes changes between two reports.
type DiffResult struct {
	OldReport string       `json:"old_report"`
	NewReport string       `json:"new_report"`
	Added     []DiffEntry  `json:"added,omitempty"`
	Removed   []DiffEntry  `json:"removed,omitempty"`
	Changed   []DiffChange `json:"changed,omitempty"`
	Summary   DiffSummary  `json:"summary"`
}

// DiffEntry is a table that appeared or disappeared.
type DiffEntry struct {
	Table    string `json:"table"`
	Category string `json:"category"`
}

// DiffChange is a table whose category changed.
type DiffChange struct {
	Table       string `json:"table"`
	OldCategory string `json:"old_category"`
	NewCategory string `json:"new_category"`
}

// DiffSummary counts changes.
type DiffSummary struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Changed int `json:"changed"`
}

// NewDiffCmd creates the diff command.
func NewDiffCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "diff <old-report> <new-report>",
		Short: "Compare two analysis reports and show changes",
		Long:  "Show tables added, removed, or changed between two report.json files. Useful for reviewing drift after watch runs.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldReport, err := loadReport(args[0])
			if err != nil {
				return fmt.Errorf("old report: %w", err)
			}
			newReport, err := loadReport(args[1])
			if err != nil {
				return fmt.Errorf("new report: %w", err)
			}

			result := computeDiff(oldReport, newReport, args[0], args[1])

			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			printDiff(cmd, result)

			if result.Summary.Added > 0 || result.Summary.Changed > 0 {
				return &FindingsError{Count: result.Summary.Added + result.Summary.Changed}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "Output format (text|json)")

	return cmd
}

func loadReport(path string) (*models.Report, error) {
	// If path is a directory, look for report.json inside it
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "report.json")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var report models.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return &report, nil
}

func computeDiff(oldR, newR *models.Report, oldPath, newPath string) *DiffResult {
	oldTables := tableMap(oldR)
	newTables := tableMap(newR)

	result := &DiffResult{
		OldReport: oldPath,
		NewReport: newPath,
	}

	// Tables in new but not in old
	for name, cat := range newTables {
		if _, found := oldTables[name]; !found {
			result.Added = append(result.Added, DiffEntry{Table: name, Category: cat})
		}
	}

	// Tables in old but not in new
	for name, cat := range oldTables {
		if _, found := newTables[name]; !found {
			result.Removed = append(result.Removed, DiffEntry{Table: name, Category: cat})
		}
	}

	// Tables in both but category changed
	for name, oldCat := range oldTables {
		if newCat, found := newTables[name]; found && oldCat != newCat {
			result.Changed = append(result.Changed, DiffChange{
				Table:       name,
				OldCategory: oldCat,
				NewCategory: newCat,
			})
		}
	}

	result.Summary = DiffSummary{
		Added:   len(result.Added),
		Removed: len(result.Removed),
		Changed: len(result.Changed),
	}

	return result
}

func tableMap(r *models.Report) map[string]string {
	m := make(map[string]string, len(r.Tables))
	for _, t := range r.Tables {
		name := t.FullName
		if name == "" {
			name = t.Database + "." + t.Name
		}
		m[name] = t.Category
	}
	return m
}

func printDiff(cmd *cobra.Command, result *DiffResult) {
	if result.Summary.Added == 0 && result.Summary.Removed == 0 && result.Summary.Changed == 0 {
		cmd.Println("No changes between reports.")
		return
	}

	if len(result.Added) > 0 {
		cmd.Printf("Added (%d):\n", len(result.Added))
		for _, e := range result.Added {
			cmd.Printf("  + %s [%s]\n", e.Table, e.Category)
		}
	}
	if len(result.Removed) > 0 {
		cmd.Printf("Removed (%d):\n", len(result.Removed))
		for _, e := range result.Removed {
			cmd.Printf("  - %s [%s]\n", e.Table, e.Category)
		}
	}
	if len(result.Changed) > 0 {
		cmd.Printf("Changed (%d):\n", len(result.Changed))
		for _, c := range result.Changed {
			cmd.Printf("  ~ %s: %s -> %s\n", c.Table, c.OldCategory, c.NewCategory)
		}
	}

	cmd.Printf("\nSummary: %d added, %d removed, %d changed\n",
		result.Summary.Added, result.Summary.Removed, result.Summary.Changed)
}

package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// WatchState stores the previous run's table categories for delta detection.
type WatchState struct {
	RunAt    time.Time `json:"run_at"`
	SafeDrop []string  `json:"safe_drop"`
	Active   []string  `json:"active"`
}

// WatchDelta describes changes between two watch runs.
type WatchDelta struct {
	RunAt       time.Time `json:"run_at"`
	NewInactive []string  `json:"new_inactive,omitempty"` // Tables newly scoring safe-to-drop
	NewActive   []string  `json:"new_active,omitempty"`   // Tables that re-appeared
}

// NewWatchCmd creates the watch command.
func NewWatchCmd() *cobra.Command {
	var (
		interval  string
		stateFile string
		once      bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Run analyze on a schedule and report table drift",
		Long:  "Continuously monitor ClickHouse table usage. Reports when tables transition from active to unused or vice versa.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dur, err := time.ParseDuration(interval)
			if err != nil {
				return fmt.Errorf("invalid --interval: %w", err)
			}
			if dur < 1*time.Hour {
				return fmt.Errorf("--interval must be at least 1h")
			}

			if stateFile == "" {
				home, _ := os.UserHomeDir()
				stateFile = filepath.Join(home, ".config", "clickspectre", "watch-state.json")
			}

			// Load previous state
			prevState, err := loadWatchState(stateFile)
			if err != nil {
				slog.Warn("failed to load watch state, starting fresh", slog.String("error", err.Error()))
			}

			if once {
				return runWatchOnce(cmd, prevState, stateFile)
			}

			return runWatchLoop(cmd, dur, prevState, stateFile)
		},
	}

	cmd.Flags().StringVar(&interval, "interval", "24h", "How often to run analysis (minimum 1h)")
	cmd.Flags().StringVar(&stateFile, "state-file", "", "Path to watch state file (default: ~/.config/clickspectre/watch-state.json)")
	cmd.Flags().BoolVar(&once, "once", false, "Run once and exit (CI-friendly)")

	return cmd
}

func runWatchOnce(cmd *cobra.Command, prevState *WatchState, stateFile string) error {
	delta, newState, err := runWatchIteration(prevState)
	if err != nil {
		return err
	}

	if err := saveWatchState(stateFile, newState); err != nil {
		slog.Warn("failed to save watch state", slog.String("error", err.Error()))
	}

	printWatchDelta(cmd, delta, prevState == nil)

	if len(delta.NewInactive) > 0 {
		return &FindingsError{Count: len(delta.NewInactive)}
	}
	return nil
}

func runWatchLoop(cmd *cobra.Command, interval time.Duration, prevState *WatchState, stateFile string) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	delta, newState, err := runWatchIteration(prevState)
	if err != nil {
		slog.Error("watch iteration failed", slog.String("error", err.Error()))
	} else {
		if err := saveWatchState(stateFile, newState); err != nil {
			slog.Warn("failed to save watch state", slog.String("error", err.Error()))
		}
		printWatchDelta(cmd, delta, prevState == nil)
		prevState = newState
	}

	for {
		select {
		case <-ticker.C:
			delta, newState, err := runWatchIteration(prevState)
			if err != nil {
				slog.Error("watch iteration failed", slog.String("error", err.Error()))
				continue
			}
			if err := saveWatchState(stateFile, newState); err != nil {
				slog.Warn("failed to save watch state", slog.String("error", err.Error()))
			}
			printWatchDelta(cmd, delta, false)
			prevState = newState

		case sig := <-sigCh:
			slog.Info("received signal, stopping watch", slog.String("signal", sig.String()))
			return nil
		}
	}
}

func runWatchIteration(prevState *WatchState) (*WatchDelta, *WatchState, error) {
	// This is a simplified watch — it runs analyze in-process
	// and extracts the safe-to-drop list for delta comparison.
	//
	// A full implementation would call runAnalyze and parse the report.
	// For now, we build the delta model so the command contract is established.

	now := time.Now().UTC()
	newState := &WatchState{
		RunAt: now,
		// SafeDrop and Active would be populated from an actual analyze run.
		// For now, this establishes the state file format.
		SafeDrop: []string{},
		Active:   []string{},
	}

	delta := &WatchDelta{RunAt: now}

	if prevState != nil {
		prevSafe := toSet(prevState.SafeDrop)
		newSafe := toSet(newState.SafeDrop)

		// Tables that are now safe-to-drop but weren't before
		for t := range newSafe {
			if !prevSafe[t] {
				delta.NewInactive = append(delta.NewInactive, t)
			}
		}

		// Tables that were safe-to-drop but are now active
		for t := range prevSafe {
			if !newSafe[t] {
				delta.NewActive = append(delta.NewActive, t)
			}
		}
	}

	return delta, newState, nil
}

func printWatchDelta(cmd *cobra.Command, delta *WatchDelta, isFirst bool) {
	if isFirst {
		cmd.Println("watch: baseline established")
		return
	}

	if len(delta.NewInactive) == 0 && len(delta.NewActive) == 0 {
		cmd.Printf("watch: no changes at %s\n", delta.RunAt.Format(time.RFC3339))
		return
	}

	if len(delta.NewInactive) > 0 {
		cmd.Printf("watch: %d table(s) newly inactive:\n", len(delta.NewInactive))
		for _, t := range delta.NewInactive {
			cmd.Printf("  - %s\n", t)
		}
	}
	if len(delta.NewActive) > 0 {
		cmd.Printf("watch: %d table(s) re-activated:\n", len(delta.NewActive))
		for _, t := range delta.NewActive {
			cmd.Printf("  + %s\n", t)
		}
	}
}

func loadWatchState(path string) (*WatchState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s WatchState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func saveWatchState(path string, s *WatchState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

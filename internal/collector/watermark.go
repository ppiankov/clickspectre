package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Watermark tracks the last successful collection time per node.
type Watermark struct {
	LastRun time.Time            `json:"last_run"`
	Nodes   map[string]time.Time `json:"nodes,omitempty"`
}

// DefaultWatermarkPath returns the default watermark file path.
func DefaultWatermarkPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".clickspectre-watermark.json"
	}
	return filepath.Join(home, ".config", "clickspectre", "watermark.json")
}

// LoadWatermark reads a watermark from disk. Returns nil if the file doesn't exist.
func LoadWatermark(path string) (*Watermark, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read watermark: %w", err)
	}

	var w Watermark
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, fmt.Errorf("parse watermark: %w", err)
	}
	return &w, nil
}

// SaveWatermark writes the watermark to disk, creating parent directories as needed.
func SaveWatermark(path string, w *Watermark) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create watermark directory: %w", err)
	}

	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal watermark: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write watermark: %w", err)
	}
	return nil
}

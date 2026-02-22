package app

import (
	"log/slog"
	"os"
	"path/filepath"
)

const (
	markerFileName = "first_run_completed"
	appName        = "clickspectre"
)

// GetAppConfigDir returns the path to the application's configuration directory.
func GetAppConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appConfigDir := filepath.Join(configDir, appName)
	return appConfigDir, nil
}

// IsFirstRun checks if this is the first time the application is run.
// It returns true if the marker file does not exist, and creates the marker file.
// It returns false if the marker file already exists.
func IsFirstRun() bool {
	appConfigDir, err := GetAppConfigDir()
	if err != nil {
		slog.Error("failed to get app config directory", slog.String("error", err.Error()))
		return false // Assume not first run on error
	}

	markerFilePath := filepath.Join(appConfigDir, markerFileName)

	// Check if marker file exists
	if _, err := os.Stat(markerFilePath); os.IsNotExist(err) {
		// Marker file does not exist, so it's the first run.
		// Create the directory if it doesn't exist
		if err := os.MkdirAll(appConfigDir, 0755); err != nil {
			slog.Error("failed to create app config directory", slog.String("path", appConfigDir), slog.String("error", err.Error()))
			return false
		}
		// Create the marker file
		if _, err := os.Create(markerFilePath); err != nil {
			slog.Error("failed to create first run marker file", slog.String("path", markerFilePath), slog.String("error", err.Error()))
			return false
		}
		slog.Debug("first run detected and marker created", slog.String("path", markerFilePath))
		return true
	} else if err != nil {
		slog.Error("failed to check first run marker file", slog.String("path", markerFilePath), slog.String("error", err.Error()))
		return false // Assume not first run on other errors
	}

	// Marker file exists, not the first run
	slog.Debug("marker file exists, not first run", slog.String("path", markerFilePath))
	return false
}

package reporter

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// WriteAssets writes all static assets from web/ directory to the output directory
func WriteAssets(outputDir string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Find the web directory (relative to project root)
	webDir := findWebDir()
	if webDir == "" {
		return fmt.Errorf("web directory not found")
	}

	// Copy files from web/ to output directory
	files := []string{"index.html", "app.js", "styles.css"}
	for _, file := range files {
		src := filepath.Join(webDir, file)
		dst := filepath.Join(outputDir, file)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy %s: %w", file, err)
		}
	}

	// Copy libs directory
	libsSrc := filepath.Join(webDir, "libs")
	libsDst := filepath.Join(outputDir, "libs")
	if err := os.MkdirAll(libsDst, 0755); err != nil {
		return fmt.Errorf("failed to create libs directory: %w", err)
	}

	d3Src := filepath.Join(libsSrc, "d3.v7.min.js")
	d3Dst := filepath.Join(libsDst, "d3.v7.min.js")
	if err := copyFile(d3Src, d3Dst); err != nil {
		return fmt.Errorf("failed to copy d3.v7.min.js: %w", err)
	}

	log.Printf("Static assets written to: %s", outputDir)

	return nil
}

// findWebDir finds the web directory relative to the current working directory
func findWebDir() string {
	// Try several common locations
	candidates := []string{
		"web",
		"./web",
		"../web",
		"../../web",
	}

	for _, dir := range candidates {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}

	return ""
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, 0644)
}

package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestInitSetsDefaultLevel(t *testing.T) {
	original := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(original)
	})

	Init(false)
	logger := slog.Default()
	if logger.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatalf("expected info to be disabled by default")
	}
	if !logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Fatalf("expected warn to be enabled by default")
	}

	Init(true)
	logger = slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("expected debug to be enabled with verbose")
	}
}

func TestInitSuppressesInfoByDefault(t *testing.T) {
	original := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(original)
	})

	output := captureStderr(t, func() {
		Init(false)
		slog.Info("hidden info", slog.String("component", "test"))
		slog.Error("visible error", slog.String("component", "test"))
	})

	if strings.Contains(output, "hidden info") {
		t.Fatalf("expected info to be suppressed, got %q", output)
	}
	if !strings.Contains(output, "visible error") {
		t.Fatalf("expected error output, got %q", output)
	}
	if !strings.Contains(output, "level=ERROR") {
		t.Fatalf("expected text handler output, got %q", output)
	}
}

func TestInitEnablesDebugWithVerbose(t *testing.T) {
	original := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(original)
	})

	output := captureStderr(t, func() {
		Init(true)
		slog.Debug("debug enabled", slog.Int("count", 1))
	})

	if !strings.Contains(output, "debug enabled") {
		t.Fatalf("expected debug output, got %q", output)
	}
	if !strings.Contains(output, "count=1") {
		t.Fatalf("expected structured debug field, got %q", output)
	}
	if !strings.Contains(output, "level=DEBUG") {
		t.Fatalf("expected debug level in text handler output, got %q", output)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create stderr pipe: %v", err)
	}

	os.Stderr = writer
	fn()
	os.Stderr = originalStderr

	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close stderr writer: %v", err)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read stderr output: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("failed to close stderr reader: %v", err)
	}

	return string(output)
}

func TestInitQuietSuppressesWarn(t *testing.T) {
	original := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(original)
	})

	output := captureStderr(t, func() {
		Init(false, WithQuiet())
		slog.Warn("suppressed warning")
		slog.Error("visible error", slog.String("component", "test"))
	})

	if strings.Contains(output, "suppressed warning") {
		t.Fatalf("expected warn to be suppressed in quiet mode, got %q", output)
	}
	if !strings.Contains(output, "visible error") {
		t.Fatalf("expected error output in quiet mode, got %q", output)
	}
}

func TestInitQuietOverridesVerbose(t *testing.T) {
	original := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(original)
	})

	Init(true, WithQuiet())
	logger := slog.Default()
	if logger.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("expected debug to be disabled when quiet overrides verbose")
	}
	if !logger.Enabled(context.Background(), slog.LevelError) {
		t.Fatalf("expected error to remain enabled in quiet mode")
	}
}

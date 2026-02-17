package logging

import (
	"context"
	"log/slog"
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

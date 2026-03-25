package logging

import (
	"log/slog"
	"os"
)

// Init configures the global slog logger.
// quiet suppresses all output except errors. verbose enables debug output.
// quiet takes precedence over verbose.
func Init(verbose bool, opts ...Option) {
	var cfg options
	for _, o := range opts {
		o(&cfg)
	}

	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	if cfg.quiet {
		level = slog.LevelError
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

// Option configures logging behavior.
type Option func(*options)

type options struct {
	quiet bool
}

// WithQuiet suppresses all output below error level.
func WithQuiet() Option {
	return func(o *options) { o.quiet = true }
}

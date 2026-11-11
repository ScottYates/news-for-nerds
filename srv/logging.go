package srv

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// SetupLogging configures slog to write to both stderr and a log file.
// Creates the log directory if needed. Returns a cleanup function to close the file.
func SetupLogging(logFile, logLevel string) (cleanup func(), err error) {
	// Parse level
	var level slog.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// If no log file specified, just configure level on stderr
	if logFile == "" {
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(handler))
		return func() {}, nil
	}

	// Ensure log directory exists
	logDir := filepath.Dir(logFile)
	if err := os.MkdirAll(logDir, 0750); err != nil {
		return nil, err
	}

	// Open log file (append, create if needed)
	// Owner-only: the log holds user IDs, feed URLs and error details.
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	// Tighten a file created by an earlier version with 0644.
	_ = f.Chmod(0600)

	// Write to both stderr and log file
	multiWriter := io.MultiWriter(os.Stderr, f)
	handler := slog.NewTextHandler(multiWriter, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	return func() { f.Close() }, nil
}

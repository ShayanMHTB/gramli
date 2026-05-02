package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

func Setup(level, file string, quiet bool) (*os.File, error) {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	var writers []io.Writer
	if !quiet {
		writers = append(writers, os.Stderr)
	}
	var f *os.File
	if file != "" {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, err
		}
		var err error
		f, err = os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		writers = append(writers, f)
	}
	if len(writers) == 0 {
		writers = append(writers, io.Discard)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: slogLevel})))
	return f, nil
}

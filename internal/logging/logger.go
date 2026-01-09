package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"bronivik/internal/config"
	"github.com/rs/zerolog"
)

// New constructs a zerolog logger based on config settings.
// Defaults to JSON, info level, stdout when fields are empty.
func New(cfg config.LoggingConfig, app config.AppConfig) (*zerolog.Logger, io.Closer, error) {
	level := zerolog.InfoLevel
	if parsed, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(cfg.Level))); err == nil {
		level = parsed
	}

	output := io.Writer(os.Stdout)
	var closer io.Closer

	switch strings.ToLower(strings.TrimSpace(cfg.Output)) {
	case "stderr":
		output = os.Stderr
	case "file":
		if cfg.FilePath == "" {
			return nil, nil, fmt.Errorf("logging.output=file requires logging.file_path")
		}
		file, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("open log file: %w", err)
		}
		output = file
		closer = file
	}

	if strings.ToLower(strings.TrimSpace(cfg.Format)) == "console" {
		output = zerolog.ConsoleWriter{Out: output, TimeFormat: time.RFC3339}
	}

	zerolog.TimeFieldFormat = time.RFC3339Nano
	base := zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Str("app", app.Name).
		Str("env", app.Environment).
		Str("version", app.Version).
		Logger()

	return &base, closer, nil
}

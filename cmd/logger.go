package cmd

import (
	"log/slog"
	"os"
)

func newLogger(environment string) *slog.Logger {
	level := slog.LevelInfo

	if environment == "local" {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

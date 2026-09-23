package cmd

import (
	"log/slog"
	"os"
)

func newLogger(environment string) *slog.Logger {
	level := slog.LevelInfo
	switch environment {
	case "local":
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}

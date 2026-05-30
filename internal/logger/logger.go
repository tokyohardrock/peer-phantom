package logger

import (
	"log/slog"
	"os"
)

func InitLogger(f *os.File) *slog.Logger {
	log := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return log
}

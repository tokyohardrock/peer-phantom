package logger

import (
	"log/slog"
	"os"
)

func InitLogger() *slog.Logger {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return log
}

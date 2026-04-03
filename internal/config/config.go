package config

import (
	"log/slog"
	"os"
	"path/filepath"
)

type Config struct {
	RootDir         string
	DataDir         string
	HTTPAddr        string
	LogLevel        slog.Level
	FrontendDistDir string
}

func Load() Config {
	rootDir := getenv("STACKLAB_ROOT", defaultRootDir())
	return Config{
		RootDir:         rootDir,
		DataDir:         getenv("STACKLAB_DATA_DIR", filepath.Join(".local", "var", "lib", "stacklab")),
		HTTPAddr:        getenv("STACKLAB_HTTP_ADDR", "127.0.0.1:8080"),
		LogLevel:        parseLogLevel(getenv("STACKLAB_LOG_LEVEL", "info")),
		FrontendDistDir: getenv("STACKLAB_FRONTEND_DIST", filepath.Join("frontend", "dist")),
	}
}

func defaultRootDir() string {
	return filepath.Join(".local", "stacklab")
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}

	return fallback
}

func parseLogLevel(value string) slog.Level {
	switch value {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

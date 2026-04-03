package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	RootDir                 string
	DataDir                 string
	DatabasePath            string
	HTTPAddr                string
	LogLevel                slog.Level
	FrontendDistDir         string
	BootstrapPassword       string
	SessionCookieName       string
	SessionIdleTimeout      time.Duration
	SessionAbsoluteLifetime time.Duration
	CookieSecure            bool
}

func Load() Config {
	rootDir := getenv("STACKLAB_ROOT", defaultRootDir())
	dataDir := getenv("STACKLAB_DATA_DIR", filepath.Join(".local", "var", "lib", "stacklab"))
	return Config{
		RootDir:                 rootDir,
		DataDir:                 dataDir,
		DatabasePath:            getenv("STACKLAB_DATABASE_PATH", filepath.Join(dataDir, "stacklab.db")),
		HTTPAddr:                getenv("STACKLAB_HTTP_ADDR", "127.0.0.1:8080"),
		LogLevel:                parseLogLevel(getenv("STACKLAB_LOG_LEVEL", "info")),
		FrontendDistDir:         getenv("STACKLAB_FRONTEND_DIST", filepath.Join("frontend", "dist")),
		BootstrapPassword:       getenv("STACKLAB_BOOTSTRAP_PASSWORD", ""),
		SessionCookieName:       getenv("STACKLAB_SESSION_COOKIE_NAME", "stacklab_session"),
		SessionIdleTimeout:      parseDuration(getenv("STACKLAB_SESSION_IDLE_TIMEOUT", "12h"), 12*time.Hour),
		SessionAbsoluteLifetime: parseDuration(getenv("STACKLAB_SESSION_ABSOLUTE_LIFETIME", "168h"), 7*24*time.Hour),
		CookieSecure:            parseBool(getenv("STACKLAB_COOKIE_SECURE", "false")),
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

func parseDuration(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseBool(value string) bool {
	switch value {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}

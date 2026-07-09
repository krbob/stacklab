package config

import (
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	RootDir                      string
	DataDir                      string
	DatabasePath                 string
	HTTPAddr                     string
	LogLevel                     slog.Level
	FrontendDistDir              string
	BootstrapPassword            string
	SystemdUnitName              string
	DockerSystemdUnitName        string
	DockerDaemonConfigPath       string
	DockerAdminHelperPath        string
	DockerAdminUseSudo           bool
	DockerAdminBackupDir         string
	SelfUpdateHelperPath         string
	SelfUpdateUseSudo            bool
	SelfUpdatePackageName        string
	SelfUpdateHealthURL          string
	WorkspaceAdminHelperPath     string
	WorkspaceAdminUseSudo        bool
	WorkspaceAdminRepairStrategy string
	HostPublicIPLookupEnabled    bool
	SessionCookieName            string
	SessionIdleTimeout           time.Duration
	SessionAbsoluteLifetime      time.Duration
	CookieSecure                 bool
	TrustedProxies               []netip.Prefix
	LoginMaxFailures             int
	LoginFailureWindow           time.Duration
	LoginLockoutDuration         time.Duration
	StackActionTimeout           time.Duration
	DockerRegistryAuthTimeout    time.Duration
}

func Load() Config {
	rootDir := getenv("STACKLAB_ROOT", defaultRootDir())
	dataDir := getenv("STACKLAB_DATA_DIR", filepath.Join(".local", "var", "lib", "stacklab"))
	return Config{
		RootDir:                      rootDir,
		DataDir:                      dataDir,
		DatabasePath:                 getenv("STACKLAB_DATABASE_PATH", filepath.Join(dataDir, "stacklab.db")),
		HTTPAddr:                     getenv("STACKLAB_HTTP_ADDR", "127.0.0.1:8080"),
		LogLevel:                     parseLogLevel(getenv("STACKLAB_LOG_LEVEL", "info")),
		FrontendDistDir:              getenv("STACKLAB_FRONTEND_DIST", filepath.Join("frontend", "dist")),
		BootstrapPassword:            getenv("STACKLAB_BOOTSTRAP_PASSWORD", ""),
		SystemdUnitName:              getenv("STACKLAB_SYSTEMD_UNIT", "stacklab"),
		DockerSystemdUnitName:        getenv("STACKLAB_DOCKER_SYSTEMD_UNIT", "docker.service"),
		DockerDaemonConfigPath:       getenv("STACKLAB_DOCKER_DAEMON_CONFIG_PATH", "/etc/docker/daemon.json"),
		DockerAdminHelperPath:        getenv("STACKLAB_DOCKER_ADMIN_HELPER_PATH", ""),
		DockerAdminUseSudo:           parseBool(getenv("STACKLAB_DOCKER_ADMIN_USE_SUDO", "false")),
		DockerAdminBackupDir:         getenv("STACKLAB_DOCKER_ADMIN_BACKUP_DIR", filepath.Join(dataDir, "docker-admin")),
		SelfUpdateHelperPath:         getenv("STACKLAB_SELF_UPDATE_HELPER_PATH", ""),
		SelfUpdateUseSudo:            parseBool(getenv("STACKLAB_SELF_UPDATE_USE_SUDO", "false")),
		SelfUpdatePackageName:        getenv("STACKLAB_SELF_UPDATE_PACKAGE_NAME", "stacklab"),
		SelfUpdateHealthURL:          getenv("STACKLAB_SELF_UPDATE_HEALTH_URL", "http://127.0.0.1:8080/api/health"),
		WorkspaceAdminHelperPath:     getenv("STACKLAB_WORKSPACE_ADMIN_HELPER_PATH", ""),
		WorkspaceAdminUseSudo:        parseBool(getenv("STACKLAB_WORKSPACE_ADMIN_USE_SUDO", "false")),
		WorkspaceAdminRepairStrategy: getenv("STACKLAB_WORKSPACE_ADMIN_REPAIR_STRATEGY", "ownership"),
		HostPublicIPLookupEnabled:    parseBool(getenv("STACKLAB_HOST_PUBLIC_IP_LOOKUP_ENABLED", "false")),
		SessionCookieName:            getenv("STACKLAB_SESSION_COOKIE_NAME", "stacklab_session"),
		SessionIdleTimeout:           parseDuration(getenv("STACKLAB_SESSION_IDLE_TIMEOUT", "12h"), 12*time.Hour),
		SessionAbsoluteLifetime:      parseDuration(getenv("STACKLAB_SESSION_ABSOLUTE_LIFETIME", "168h"), 7*24*time.Hour),
		CookieSecure:                 parseBool(getenv("STACKLAB_COOKIE_SECURE", "false")),
		TrustedProxies:               parseTrustedProxies(getenv("STACKLAB_TRUSTED_PROXIES", "")),
		LoginMaxFailures:             parseInt(getenv("STACKLAB_LOGIN_MAX_FAILURES", "5"), 5),
		LoginFailureWindow:           parseDuration(getenv("STACKLAB_LOGIN_FAILURE_WINDOW", "5m"), 5*time.Minute),
		LoginLockoutDuration:         parseDuration(getenv("STACKLAB_LOGIN_LOCKOUT_DURATION", "5m"), 5*time.Minute),
		StackActionTimeout:           parseDuration(getenv("STACKLAB_STACK_ACTION_TIMEOUT", "30m"), 30*time.Minute),
		DockerRegistryAuthTimeout:    parseDuration(getenv("STACKLAB_DOCKER_REGISTRY_AUTH_TIMEOUT", "5m"), 5*time.Minute),
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

func parseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseTrustedProxies(value string) []netip.Prefix {
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(trimmed); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(trimmed); err == nil {
			prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return prefixes
}

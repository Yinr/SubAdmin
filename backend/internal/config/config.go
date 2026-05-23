package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	Addr         string
	DBPath       string
	LogDir       string
	LoginSecret  string
	SecretKey    string
	BasePath     string
	CookiePath   string
	CookieSecure bool
	SessionTTL   time.Duration
}

func Load() Config {
	addr := os.Getenv("SUBADMIN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8787"
	}

	dbPath := os.Getenv("SUBADMIN_DB_PATH")
	if dbPath == "" {
		dbPath = "../data/subadmin.db"
	}

	logDir := os.Getenv("SUBADMIN_LOG_DIR")
	if logDir == "" {
		logDir = "data/logs"
	}
	logDir = resolveProjectRelativePath(logDir)

	basePath := normalizeBasePath(os.Getenv("SUBADMIN_BASE_PATH"))

	cookieSecure := true
	if raw := os.Getenv("SUBADMIN_COOKIE_SECURE"); raw != "" {
		if parsed, err := strconv.ParseBool(raw); err == nil {
			cookieSecure = parsed
		}
	}

	sessionTTL := 24 * time.Hour
	if raw := os.Getenv("SUBADMIN_SESSION_TTL"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			sessionTTL = parsed
		}
	}

	return Config{
		Addr:         addr,
		DBPath:       dbPath,
		LogDir:       logDir,
		LoginSecret:  os.Getenv("SUBADMIN_LOGIN_SECRET"),
		SecretKey:    os.Getenv("SUBADMIN_SECRET_KEY"),
		BasePath:     basePath,
		CookiePath:   basePath,
		CookieSecure: cookieSecure,
		SessionTTL:   sessionTTL,
	}
}

func normalizeBasePath(value string) string {
	if value == "" || value == "/" {
		return "/"
	}
	if value[0] != '/' {
		value = "/" + value
	}
	for len(value) > 1 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}

func resolveProjectRelativePath(value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	if wd, err := os.Getwd(); err == nil {
		if filepath.Base(wd) == "backend" {
			return filepath.Clean(filepath.Join(filepath.Dir(wd), value))
		}
		return filepath.Clean(filepath.Join(wd, value))
	}
	return filepath.Clean(value)
}

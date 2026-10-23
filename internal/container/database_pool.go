package container

import (
	"strconv"
	"strings"
)

// Explicit pool limits are optional. Zero open connections keeps database/sql's
// unlimited default; zero idle connections disables the idle cache.
const maxConfiguredDatabaseConnections = 1000

type databasePoolLimits struct {
	MaxOpen int
	MaxIdle int
}

func parseDatabasePoolLimit(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return fallback
		}
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n > maxConfiguredDatabaseConnections {
		return maxConfiguredDatabaseConnections
	}
	return int(n)
}

func databasePoolLimitsFromEnv(driver string, getenv func(string) string) databasePoolLimits {
	if driver == "sqlite" {
		// Preserve SQLite's single writer even if shared deployment env vars
		// request a larger PostgreSQL/MySQL pool.
		return databasePoolLimits{MaxOpen: 1, MaxIdle: 1}
	}
	limits := databasePoolLimits{
		MaxOpen: parseDatabasePoolLimit(getenv("WEKNORA_DB_MAX_OPEN_CONNS"), 0),
		MaxIdle: parseDatabasePoolLimit(getenv("WEKNORA_DB_MAX_IDLE_CONNS"), 10),
	}
	if limits.MaxOpen > 0 && limits.MaxIdle > limits.MaxOpen {
		limits.MaxIdle = limits.MaxOpen
	}
	return limits
}

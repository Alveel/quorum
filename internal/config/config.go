package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL       string
	AdminGroups       []string
	TeamSize          int
	MinPresentDefault int
	Port              string
	DevAuthBypass     bool
	DevUser           string
	DevAdmin          bool
}

func Load() Config {
	c := Config{
		DatabaseURL:       mustEnv("DATABASE_URL"),
		Port:              envOr("PORT", "8080"),
		MinPresentDefault: intEnvOr("MIN_PRESENT_DEFAULT", 8),
		TeamSize:          intEnvOr("TEAM_SIZE", 15),
		DevAuthBypass:     os.Getenv("DEV_AUTH_BYPASS") == "true",
		DevUser:           os.Getenv("DEV_USER"),
		DevAdmin:          os.Getenv("DEV_ADMIN") == "true",
	}
	if groups := os.Getenv("ADMIN_GROUPS"); groups != "" {
		for _, g := range strings.Split(groups, ",") {
			if g = strings.TrimSpace(g); g != "" {
				c.AdminGroups = append(c.AdminGroups, g)
			}
		}
	}
	return c
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" && key == "DATABASE_URL" {
		// Allow empty for test builds; checked at startup.
		return ""
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func intEnvOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

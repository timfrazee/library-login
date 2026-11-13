package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const (
	envData  = "LIBRARY_LOGIN_DATA"
	envLocal = "LIBRARY_LOGIN_LOCAL"
	envHost  = "LIBRARY_LOGIN_HOST"
	envLan   = "LIBRARY_LOGIN_LAN"
)

// loadDotEnv loads KEY=VALUE lines from a .env file into the process environment
// without overriding variables already set, so the real environment and flags
// still take precedence. A missing file is not an error.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key != "" {
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, val)
			}
		}
	}
}

// dataDirFromConfig resolves the data directory by precedence: -data flag, then
// LIBRARY_LOGIN_DATA (explicit path), then LIBRARY_LOGIN_LOCAL (current working
// directory — the dev toggle), then the per-user config directory.
func dataDirFromConfig(flagData string) string {
	if flagData != "" {
		return flagData
	}
	if d := os.Getenv(envData); d != "" {
		return d
	}
	if truthy(os.Getenv(envLocal)) {
		if cwd, err := os.Getwd(); err == nil {
			return cwd
		}
	}
	if base, err := os.UserConfigDir(); err == nil {
		return filepath.Join(base, "library-login")
	}
	return "."
}

// hostFromConfig resolves a bind-address override by precedence: -host flag,
// LIBRARY_LOGIN_HOST, then the -lan flag / LIBRARY_LOGIN_LAN (both mean 0.0.0.0).
// An empty result means "use the platform default".
func hostFromConfig(flagHost string, lanFlag bool) string {
	if flagHost != "" {
		return flagHost
	}
	if h := os.Getenv(envHost); h != "" {
		return h
	}
	if lanFlag || truthy(os.Getenv(envLan)) {
		return "0.0.0.0"
	}
	return ""
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

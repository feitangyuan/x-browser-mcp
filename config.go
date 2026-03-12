package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               string
	Headless           bool
	BrowserBin         string
	RemoteDebugURL     string
	UserDataDir        string
	ProfileDirectory   string
	CookiesPath        string
	CacheTTL           time.Duration
	MinSearchInterval  time.Duration
	SearchBudgetWindow time.Duration
	MaxLiveSearches    int
	SearchTimeout      time.Duration
	LoginTimeout       time.Duration
	ManagedBrowserLabel string
	ManagedBrowserPlist string
}

func DefaultConfig() Config {
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}
	user := sanitizeIdentifier(os.Getenv("USER"))
	label := ""
	plist := ""
	if user != "" {
		label = fmt.Sprintf("com.%s.x-browser-mcp-browser", user)
		plist = filepath.Join(homeDir, "Library", "LaunchAgents", label+".plist")
	}

	return Config{
		Port:               ":18110",
		Headless:           true,
		BrowserBin:         defaultBrowserBin(),
		UserDataDir:        filepath.Join(wd, "debug-chrome"),
		ProfileDirectory:   "Default",
		CookiesPath:        filepath.Join(wd, "x_session_cookies.json"),
		CacheTTL:           5 * time.Minute,
		MinSearchInterval:  15 * time.Second,
		SearchBudgetWindow: 10 * time.Minute,
		MaxLiveSearches:    8,
		SearchTimeout:      25 * time.Second,
		LoginTimeout:       4 * time.Minute,
		ManagedBrowserLabel: label,
		ManagedBrowserPlist: plist,
	}
}

func defaultBrowserBin() string {
	if envBin := strings.TrimSpace(os.Getenv("ROD_BROWSER_BIN")); envBin != "" {
		return envBin
	}
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func (c Config) ProfileMode() bool {
	if c.RemoteDebugMode() {
		return false
	}
	return strings.TrimSpace(c.UserDataDir) != ""
}

func (c Config) RemoteDebugMode() bool {
	return strings.TrimSpace(c.RemoteDebugURL) != ""
}

func (c Config) CookieMode() bool {
	return !c.RemoteDebugMode() && !c.ProfileMode()
}

func (c Config) LaunchesBrowserProcess() bool {
	return !c.RemoteDebugMode()
}

func (c Config) ManagedBrowserConfigured() bool {
	return strings.TrimSpace(c.ManagedBrowserLabel) != "" && strings.TrimSpace(c.ManagedBrowserPlist) != ""
}

func (c Config) RemoteDebugPort() int {
	raw := strings.TrimSpace(c.RemoteDebugURL)
	if raw == "" {
		return 0
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	if parsed.Port() == "" {
		return 0
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 {
		return 0
	}
	return port
}

func (c Config) SessionStoragePath() string {
	if c.RemoteDebugMode() {
		return c.RemoteDebugURL
	}
	if c.ProfileMode() {
		return c.UserDataDir
	}
	return c.CookiesPath
}

func sanitizeIdentifier(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		}
	}
	return b.String()
}

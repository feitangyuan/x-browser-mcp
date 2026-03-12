package main

import (
	"flag"

	"github.com/sirupsen/logrus"
)

func main() {
	cfg := DefaultConfig()

	flag.StringVar(&cfg.Port, "port", cfg.Port, "HTTP listen address")
	flag.BoolVar(&cfg.Headless, "headless", cfg.Headless, "Run search browsers in headless mode")
	flag.StringVar(&cfg.BrowserBin, "bin", cfg.BrowserBin, "Browser binary path")
	flag.StringVar(&cfg.RemoteDebugURL, "remote-debug-url", cfg.RemoteDebugURL, "Attach to an existing Chrome DevTools endpoint instead of launching a browser")
	flag.StringVar(&cfg.UserDataDir, "user-data-dir", cfg.UserDataDir, "Persistent Chrome user data dir to reuse across runs")
	flag.StringVar(&cfg.ProfileDirectory, "profile-directory", cfg.ProfileDirectory, "Chrome profile directory inside user-data-dir")
	flag.StringVar(&cfg.CookiesPath, "cookies", cfg.CookiesPath, "Cookie storage file path")
	flag.DurationVar(&cfg.CacheTTL, "cache-ttl", cfg.CacheTTL, "Cache TTL for repeated searches")
	flag.DurationVar(&cfg.MinSearchInterval, "min-interval", cfg.MinSearchInterval, "Minimum interval between live searches")
	flag.DurationVar(&cfg.SearchBudgetWindow, "budget-window", cfg.SearchBudgetWindow, "Rolling window for live search rate limiting")
	flag.IntVar(&cfg.MaxLiveSearches, "max-live-searches", cfg.MaxLiveSearches, "Maximum live X searches allowed per rolling window")
	flag.DurationVar(&cfg.SearchTimeout, "search-timeout", cfg.SearchTimeout, "Timeout for a single search request")
	flag.DurationVar(&cfg.LoginTimeout, "login-timeout", cfg.LoginTimeout, "Timeout for manual login")
	flag.StringVar(&cfg.ManagedBrowserLabel, "managed-browser-label", cfg.ManagedBrowserLabel, "launchd label for the managed headless browser")
	flag.StringVar(&cfg.ManagedBrowserPlist, "managed-browser-plist", cfg.ManagedBrowserPlist, "launchd plist path for the managed headless browser")
	flag.Parse()

	logrus.SetLevel(logrus.InfoLevel)

	server := NewAppServer(NewSearchService(cfg))
	if err := server.Start(cfg.Port); err != nil {
		logrus.WithError(err).Fatal("server stopped with error")
	}
}

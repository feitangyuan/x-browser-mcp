package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
)

func TestDefaultConfigUsesPersistentProfileDir(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.UserDataDir == "" {
		t.Fatal("expected default user data dir to be set")
	}
	if cfg.ProfileDirectory != "Default" {
		t.Fatalf("unexpected default profile directory: %q", cfg.ProfileDirectory)
	}
	if !cfg.ProfileMode() {
		t.Fatal("expected default config to run in profile mode")
	}
	if cfg.BrowserBin == "" {
		t.Fatal("expected default browser binary to be resolved for isolated browser mode")
	}
	if cfg.MinSearchInterval < 10*time.Second {
		t.Fatalf("expected conservative min interval, got %s", cfg.MinSearchInterval)
	}
	if cfg.CacheTTL < 3*time.Minute {
		t.Fatalf("expected longer cache ttl, got %s", cfg.CacheTTL)
	}
}

func TestCheckUserDataDirAvailabilityRejectsLiveChromeProfile(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "SingletonLock")
	if err := os.WriteFile(lockPath, []byte("busy"), 0o600); err != nil {
		t.Fatalf("failed to write lock file: %v", err)
	}

	err := CheckUserDataDirAvailability(dir)
	if err == nil {
		t.Fatal("expected lock detection to fail")
	}
	if !strings.Contains(err.Error(), "SingletonLock") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoteDebugModeOverridesProfileLaunch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemoteDebugURL = "http://127.0.0.1:9222"
	if !cfg.RemoteDebugMode() {
		t.Fatal("expected remote debug mode to be enabled")
	}
	if cfg.ProfileMode() {
		t.Fatal("expected profile mode to be disabled when remote debug url is set")
	}
	if cfg.CookieMode() {
		t.Fatal("expected cookie mode to be disabled when remote debug url is set")
	}
	if cfg.LaunchesBrowserProcess() {
		t.Fatal("expected remote debug mode to attach instead of launching/owning a browser process")
	}
}

func TestBuildManualLoginCommandUsesIsolatedProfile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BrowserBin = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	cfg.UserDataDir = "/tmp/x-browser-mcp-profile"
	cfg.ProfileDirectory = "Default"

	args := BuildManualLoginCommandArgs(cfg)
	joined := strings.Join(args, " ")

	if len(args) == 0 || args[0] != cfg.BrowserBin {
		t.Fatalf("expected browser binary first, got %v", args)
	}
	if !strings.Contains(joined, "--user-data-dir=/tmp/x-browser-mcp-profile") {
		t.Fatalf("missing isolated profile arg: %q", joined)
	}
	if !strings.Contains(joined, "--profile-directory=Default") {
		t.Fatalf("missing profile directory arg: %q", joined)
	}
	if !strings.Contains(joined, "https://x.com/home") {
		t.Fatalf("missing x home url: %q", joined)
	}
}

func TestBuildManualLoginCommandReusesRemoteDebugPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BrowserBin = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	cfg.UserDataDir = "/tmp/x-browser-mcp-profile"
	cfg.ProfileDirectory = "Default"
	cfg.RemoteDebugURL = "http://127.0.0.1:9222"

	args := BuildManualLoginCommandArgs(cfg)
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--remote-debugging-port=9222") {
		t.Fatalf("expected interactive login browser to reuse remote debug port, got %q", joined)
	}
}

func TestHasAuthenticatedXCookies(t *testing.T) {
	cookies := []*proto.NetworkCookie{
		{Name: "guest_id", Domain: ".x.com"},
		{Name: "auth_token", Domain: ".x.com"},
		{Name: "ct0", Domain: ".x.com"},
	}

	if !hasAuthenticatedXCookies(cookies) {
		t.Fatal("expected auth cookies to be recognized")
	}
}

func TestHasAuthenticatedXCookiesRejectsGuestOnly(t *testing.T) {
	cookies := []*proto.NetworkCookie{
		{Name: "guest_id", Domain: ".x.com"},
		{Name: "personalization_id", Domain: ".x.com"},
	}

	if hasAuthenticatedXCookies(cookies) {
		t.Fatal("expected guest-only cookies to be rejected")
	}
}

func TestAllowLiveSearchEnforcesRollingBudget(t *testing.T) {
	now := time.Date(2026, 3, 12, 7, 0, 0, 0, time.UTC)
	history := []time.Time{
		now.Add(-8 * time.Minute),
		now.Add(-4 * time.Minute),
	}

	updated, retryAfter, allowed := AllowLiveSearch(history, now, 10*time.Minute, 2)
	if allowed {
		t.Fatal("expected rolling budget to block search")
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retry window, got %s", retryAfter)
	}
	if len(updated) != 2 {
		t.Fatalf("expected history to keep recent entries, got %d", len(updated))
	}
}

func TestAllowLiveSearchDropsExpiredEntries(t *testing.T) {
	now := time.Date(2026, 3, 12, 7, 0, 0, 0, time.UTC)
	history := []time.Time{
		now.Add(-15 * time.Minute),
		now.Add(-4 * time.Minute),
	}

	updated, retryAfter, allowed := AllowLiveSearch(history, now, 10*time.Minute, 2)
	if !allowed {
		t.Fatal("expected search to be allowed after dropping expired entries")
	}
	if retryAfter != 0 {
		t.Fatalf("unexpected retry window: %s", retryAfter)
	}
	if len(updated) != 2 {
		t.Fatalf("expected history to contain old recent item plus new search, got %d", len(updated))
	}
}

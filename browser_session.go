package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/cdp"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
)

type ManagedBrowserController interface {
	Suspend(ctx context.Context) error
	Resume(ctx context.Context) error
}

type LaunchdManagedBrowserController struct {
	label    string
	plist    string
	domain   string
	stopWait time.Duration
	execCommand func(context.Context, string, ...string) *exec.Cmd
}

type BrowserSession struct {
	browser            *rod.Browser
	launcher           *launcher.Launcher
	remoteWebSocket    *cdp.WebSocket
	ownsBrowserProcess bool
	onClose            func()
}

func NewBrowserSession(ctx context.Context, cfg Config, headless bool) (*BrowserSession, error) {
	if cfg.RemoteDebugMode() {
		controlURL, err := resolveControlURL(cfg.RemoteDebugURL)
		if err != nil {
			return nil, err
		}
		ws := &cdp.WebSocket{}
		if err := ws.Connect(ctx, controlURL, nil); err != nil {
			return nil, err
		}
		client := cdp.New().Start(ws)
		browser := rod.New().Client(client).Context(ctx)
		if err := browser.Connect(); err != nil {
			_ = ws.Close()
			return nil, err
		}
		return &BrowserSession{
			browser:            browser,
			remoteWebSocket:    ws,
			ownsBrowserProcess: false,
		}, nil
	}

	launch := launcher.New().Headless(headless)
	if cfg.BrowserBin != "" {
		launch.Bin(cfg.BrowserBin)
	}
	if cfg.ProfileMode() {
		if err := os.MkdirAll(cfg.UserDataDir, 0o755); err != nil {
			return nil, err
		}
		if err := CheckUserDataDirAvailability(cfg.UserDataDir); err != nil {
			return nil, err
		}
		launch.Set(flags.UserDataDir, cfg.UserDataDir)
		if cfg.ProfileDirectory != "" {
			launch.Set(flags.Flag(flags.ProfileDir), cfg.ProfileDirectory)
		}
	}

	controlURL, err := launch.Launch()
	if err != nil {
		return nil, err
	}

	browser := rod.New().ControlURL(controlURL).Context(ctx)
	if err := browser.Connect(); err != nil {
		launch.Kill()
		go launch.Cleanup()
		return nil, err
	}

	return &BrowserSession{
		browser:            browser,
		launcher:           launch,
		ownsBrowserProcess: true,
	}, nil
}

func resolveControlURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if strings.HasPrefix(trimmed, "ws://") || strings.HasPrefix(trimmed, "wss://") {
		return trimmed, nil
	}

	versionURL := strings.TrimRight(trimmed, "/") + "/json/version"
	resp, err := http.Get(versionURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Chrome DevTools endpoint %s: %w", versionURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status from %s: %s %s", versionURL, resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("webSocketDebuggerUrl missing in %s", versionURL)
	}
	return payload.WebSocketDebuggerURL, nil
}

func WaitForRemoteDebugEndpoint(ctx context.Context, rawURL string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if _, err := resolveControlURL(rawURL); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *BrowserSession) NewPage() (*rod.Page, error) {
	return s.browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
}

func (s *BrowserSession) Close() {
	if s.browser != nil {
		if s.ownsBrowserProcess {
			_ = s.browser.Close()
		}
	}
	if s.remoteWebSocket != nil {
		_ = s.remoteWebSocket.Close()
	}
	if s.launcher != nil {
		s.launcher.Kill()
		if s.launcher.Get(flags.UserDataDir) == "" {
			go s.launcher.Cleanup()
		}
	}
	if s.onClose != nil {
		s.onClose()
		s.onClose = nil
	}
}

func CheckUserDataDirAvailability(userDataDir string) error {
	lockPath := filepath.Join(userDataDir, "SingletonLock")
	if _, err := os.Lstat(lockPath); err == nil {
		return fmt.Errorf("chrome profile is already in use (%s exists); close Chrome first or use a dedicated user-data-dir", lockPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func BuildManualLoginCommandArgs(cfg Config) []string {
	args := []string{cfg.BrowserBin}
	if cfg.UserDataDir != "" {
		args = append(args, "--user-data-dir="+cfg.UserDataDir)
	}
	if cfg.ProfileDirectory != "" {
		args = append(args, "--profile-directory="+cfg.ProfileDirectory)
	}
	if port := cfg.RemoteDebugPort(); port > 0 {
		args = append(args, fmt.Sprintf("--remote-debugging-port=%d", port))
	}
	args = append(args, "--no-first-run", "--no-default-browser-check")
	args = append(args, "https://x.com/home")
	return args
}

func BuildManagedBrowserCommandArgs(cfg Config) []string {
	args := []string{cfg.BrowserBin}
	if cfg.UserDataDir != "" {
		args = append(args, "--user-data-dir="+cfg.UserDataDir)
	}
	if cfg.ProfileDirectory != "" {
		args = append(args, "--profile-directory="+cfg.ProfileDirectory)
	}
	if port := cfg.RemoteDebugPort(); port > 0 {
		args = append(args, fmt.Sprintf("--remote-debugging-port=%d", port))
	}
	args = append(args, "--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-gpu", "about:blank")
	return args
}

func LaunchManualLoginBrowser(cfg Config) (*exec.Cmd, error) {
	if strings.TrimSpace(cfg.BrowserBin) == "" {
		return nil, fmt.Errorf("browser binary path is required for manual login")
	}
	if cfg.UserDataDir != "" {
		if err := os.MkdirAll(cfg.UserDataDir, 0o755); err != nil {
			return nil, err
		}
	}

	args := BuildManualLoginCommandArgs(cfg)
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func LaunchManagedHeadlessBrowser(cfg Config) (*exec.Cmd, error) {
	if strings.TrimSpace(cfg.BrowserBin) == "" {
		return nil, fmt.Errorf("browser binary path is required for managed browser launch")
	}
	if cfg.UserDataDir != "" {
		if err := os.MkdirAll(cfg.UserDataDir, 0o755); err != nil {
			return nil, err
		}
	}

	args := BuildManagedBrowserCommandArgs(cfg)
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func NewLaunchdManagedBrowserController(cfg Config) ManagedBrowserController {
	if !cfg.ManagedBrowserConfigured() {
		return nil
	}
	return &LaunchdManagedBrowserController{
		label:    cfg.ManagedBrowserLabel,
		plist:    cfg.ManagedBrowserPlist,
		domain:   fmt.Sprintf("gui/%d", os.Getuid()),
		stopWait: 8 * time.Second,
		execCommand: exec.CommandContext,
	}
}

func (c *LaunchdManagedBrowserController) Suspend(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if err := c.run(ctx, "launchctl", "bootout", c.domain, c.plist); err != nil {
		if strings.Contains(err.Error(), "No such process") || strings.Contains(err.Error(), "No such file or directory") {
			return nil
		}
		return err
	}
	return nil
}

func (c *LaunchdManagedBrowserController) Resume(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if err := c.run(ctx, "launchctl", "bootstrap", c.domain, c.plist); err != nil {
		if !strings.Contains(err.Error(), "service already loaded") {
			return err
		}
	}
	return c.run(ctx, "launchctl", "kickstart", "-k", c.domain+"/"+c.label)
}

func (c *LaunchdManagedBrowserController) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if c.execCommand != nil {
		return c.execCommand(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...)
}

func (c *LaunchdManagedBrowserController) run(ctx context.Context, name string, args ...string) error {
	cmd := c.command(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

func SaveCookies(browser *rod.Browser, cookiesPath string) error {
	cookies, err := browser.GetCookies()
	if err != nil {
		return err
	}

	data, err := json.Marshal(cookies)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cookiesPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(cookiesPath, data, 0o600)
}

func LoadCookies(page *rod.Page, cookiesPath string) error {
	data, err := os.ReadFile(cookiesPath)
	if err != nil {
		return err
	}

	var cookies []*proto.NetworkCookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		return err
	}

	params := make([]*proto.NetworkCookieParam, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		sourcePort := cookie.SourcePort
		params = append(params, &proto.NetworkCookieParam{
			Name:         cookie.Name,
			Value:        cookie.Value,
			Domain:       cookie.Domain,
			Path:         cookie.Path,
			Secure:       cookie.Secure,
			HTTPOnly:     cookie.HTTPOnly,
			SameSite:     cookie.SameSite,
			Expires:      cookie.Expires,
			Priority:     cookie.Priority,
			SameParty:    cookie.SameParty,
			SourceScheme: cookie.SourceScheme,
			SourcePort:   &sourcePort,
			PartitionKey: cookie.PartitionKey,
		})
	}

	if len(params) == 0 {
		return nil
	}
	return page.SetCookies(params)
}

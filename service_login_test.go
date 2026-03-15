package main

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeManagedBrowserController struct {
	mu           sync.Mutex
	suspendCalls int
	resumeCalls  int
	resumeErr    error
}

func (f *fakeManagedBrowserController) Suspend(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspendCalls++
	return nil
}

func (f *fakeManagedBrowserController) Resume(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumeCalls++
	return f.resumeErr
}

func (f *fakeManagedBrowserController) SuspendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspendCalls
}

func (f *fakeManagedBrowserController) ResumeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resumeCalls
}

func TestStartLoginRemoteDebugUsesVisibleLoginFlow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemoteDebugURL = "http://127.0.0.1:9222"
	cfg.UserDataDir = t.TempDir()
	cfg.ProfileDirectory = "Default"

	controller := &fakeManagedBrowserController{}
	launched := 0

	svc := NewSearchService(cfg)
	svc.managedBrowser = controller
	svc.waitForRemoteDebug = func(context.Context, string) error { return nil }
	svc.launchManualLogin = func(_ Config) (*exec.Cmd, error) {
		launched++
		cmd := exec.Command("sleep", "30")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		})
		return cmd, nil
	}
	svc.watchLoginFn = func(*loginSession) {}

	resp, err := svc.StartLogin(context.Background())
	if err != nil {
		t.Fatalf("start login failed: %v", err)
	}
	if controller.SuspendCount() != 1 {
		t.Fatalf("expected managed browser to be suspended once, got %d", controller.SuspendCount())
	}
	if launched != 1 {
		t.Fatalf("expected visible login browser to launch once, got %d", launched)
	}
	if !strings.Contains(resp.Message, "visible window") {
		t.Fatalf("expected visible login messaging, got %q", resp.Message)
	}
	if svc.login == nil {
		t.Fatal("expected login session to be recorded")
	}
}

func TestStartLoginSkipsVisibleFlowWhenAlreadyAuthenticated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemoteDebugURL = "http://127.0.0.1:9222"
	cfg.UserDataDir = t.TempDir()
	cfg.ProfileDirectory = "Default"

	controller := &fakeManagedBrowserController{}
	launched := 0

	svc := NewSearchService(cfg)
	svc.managedBrowser = controller
	svc.checkRemoteDebugLogin = func(context.Context) (bool, error) { return true, nil }
	svc.launchManualLogin = func(_ Config) (*exec.Cmd, error) {
		launched++
		t.Fatal("visible login browser should not launch when already authenticated")
		return nil, nil
	}

	resp, err := svc.StartLogin(context.Background())
	if err != nil {
		t.Fatalf("start login failed: %v", err)
	}
	if controller.SuspendCount() != 0 {
		t.Fatalf("expected managed browser to stay running, got %d suspends", controller.SuspendCount())
	}
	if launched != 0 {
		t.Fatalf("expected no visible browser launch, got %d", launched)
	}
	if !strings.Contains(resp.Message, "already authenticated") {
		t.Fatalf("expected already authenticated message, got %q", resp.Message)
	}
	if svc.login != nil {
		t.Fatal("expected no login session when already authenticated")
	}
}

func TestOpenBrowserSessionStartsManagedBrowserWhenRemoteDebugUnavailable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemoteDebugURL = "http://127.0.0.1:9222"

	controller := &fakeManagedBrowserController{}
	svc := NewSearchService(cfg)
	svc.managedBrowser = controller

	var attempts int
	expected := &BrowserSession{}
	svc.newBrowserSession = func(context.Context, Config, bool) (*BrowserSession, error) {
		attempts++
		if attempts == 1 {
			return nil, context.DeadlineExceeded
		}
		return expected, nil
	}

	waitCalls := 0
	svc.waitForRemoteDebug = func(context.Context, string) error {
		waitCalls++
		return nil
	}

	session, err := svc.openBrowserSession(context.Background(), true)
	if err != nil {
		t.Fatalf("open browser session failed: %v", err)
	}
	if session != expected {
		t.Fatal("expected retried browser session to be returned")
	}
	if attempts != 2 {
		t.Fatalf("expected browser session to retry once, got %d attempts", attempts)
	}
	if controller.ResumeCount() != 1 {
		t.Fatalf("expected managed browser resume once, got %d", controller.ResumeCount())
	}
	if waitCalls != 1 {
		t.Fatalf("expected remote debug wait once, got %d", waitCalls)
	}
}

func TestOpenBrowserSessionDoesNotStartManagedBrowserDuringRemoteLogin(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemoteDebugURL = "http://127.0.0.1:9222"

	controller := &fakeManagedBrowserController{}
	svc := NewSearchService(cfg)
	svc.managedBrowser = controller
	svc.login = &loginSession{
		deadline:             time.Now().Add(time.Minute),
		resumeManagedBrowser: true,
	}
	svc.newBrowserSession = func(context.Context, Config, bool) (*BrowserSession, error) {
		return nil, context.DeadlineExceeded
	}
	svc.waitForRemoteDebug = func(context.Context, string) error {
		t.Fatal("remote debug wait should not be called during interactive login")
		return nil
	}

	_, err := svc.openBrowserSession(context.Background(), true)
	if err == nil {
		t.Fatal("expected open browser session to fail when remote debug is unavailable")
	}
	if controller.ResumeCount() != 0 {
		t.Fatalf("expected managed browser to stay suspended, got %d resumes", controller.ResumeCount())
	}
}

func TestOpenBrowserSessionFallsBackToDirectHeadlessLaunch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.RemoteDebugURL = "http://127.0.0.1:9222"

	controller := &fakeManagedBrowserController{resumeErr: context.DeadlineExceeded}
	svc := NewSearchService(cfg)
	svc.managedBrowser = controller

	var attempts int
	expected := &BrowserSession{}
	svc.newBrowserSession = func(context.Context, Config, bool) (*BrowserSession, error) {
		attempts++
		if attempts == 1 {
			return nil, context.DeadlineExceeded
		}
		return expected, nil
	}

	waitCalls := 0
	svc.waitForRemoteDebug = func(context.Context, string) error {
		waitCalls++
		return nil
	}

	launches := 0
	svc.launchManagedBrowser = func(_ Config) (*exec.Cmd, error) {
		launches++
		cmd := exec.Command("sleep", "30")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		})
		return cmd, nil
	}

	session, err := svc.openBrowserSession(context.Background(), true)
	if err != nil {
		t.Fatalf("open browser session failed: %v", err)
	}
	if session != expected {
		t.Fatal("expected retried browser session to be returned")
	}
	if launches != 1 {
		t.Fatalf("expected direct headless launch once, got %d", launches)
	}
	if waitCalls != 1 {
		t.Fatalf("expected remote debug wait once, got %d", waitCalls)
	}
}

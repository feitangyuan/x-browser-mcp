package main

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

type fakeManagedBrowserController struct {
	mu           sync.Mutex
	suspendCalls int
	resumeCalls  int
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
	return nil
}

func (f *fakeManagedBrowserController) SuspendCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspendCalls
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

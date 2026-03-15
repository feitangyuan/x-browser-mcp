package main

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestLaunchdManagedBrowserResumeBootstrapsAndKickstarts(t *testing.T) {
	controller := &LaunchdManagedBrowserController{
		label:  "com.example.browser",
		plist:  "/tmp/com.example.browser.plist",
		domain: "gui/501",
	}

	var calls [][]string
	controller.execCommand = func(_ context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return exec.Command("true")
	}

	if err := controller.Resume(context.Background()); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	expected := [][]string{
		{"launchctl", "bootstrap", "gui/501", "/tmp/com.example.browser.plist"},
		{"launchctl", "kickstart", "-k", "gui/501/com.example.browser"},
	}
	if !reflect.DeepEqual(calls, expected) {
		t.Fatalf("unexpected launchctl calls: %#v", calls)
	}
}

func TestLaunchdManagedBrowserResumeKickstartsWhenAlreadyLoaded(t *testing.T) {
	controller := &LaunchdManagedBrowserController{
		label:  "com.example.browser",
		plist:  "/tmp/com.example.browser.plist",
		domain: "gui/501",
	}

	var calls [][]string
	controller.execCommand = func(_ context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		if len(calls) == 1 {
			return exec.Command("sh", "-c", "printf 'service already loaded' >&2; exit 1")
		}
		return exec.Command("true")
	}

	if err := controller.Resume(context.Background()); err != nil {
		t.Fatalf("resume failed: %v", err)
	}

	expected := [][]string{
		{"launchctl", "bootstrap", "gui/501", "/tmp/com.example.browser.plist"},
		{"launchctl", "kickstart", "-k", "gui/501/com.example.browser"},
	}
	if !reflect.DeepEqual(calls, expected) {
		t.Fatalf("unexpected launchctl calls: %#v", calls)
	}
}

func TestLaunchdManagedBrowserResumeReturnsBootstrapFailure(t *testing.T) {
	controller := &LaunchdManagedBrowserController{
		label:  "com.example.browser",
		plist:  "/tmp/com.example.browser.plist",
		domain: "gui/501",
	}

	var calls [][]string
	controller.execCommand = func(_ context.Context, name string, args ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, args...))
		return exec.Command("sh", "-c", "printf 'boom' >&2; exit 1")
	}

	err := controller.Resume(context.Background())
	if err == nil {
		t.Fatal("expected resume to fail")
	}
	if len(calls) != 1 {
		t.Fatalf("expected bootstrap only on failure, got %#v", calls)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
}

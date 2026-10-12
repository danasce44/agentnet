package client

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeLaunchPreservesOwnerWrapperAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installation fixture")
	}
	dir := t.TempDir()
	executable := filepath.Join(dir, "node_modules", "fixture", "cli")
	if err := os.MkdirAll(filepath.Dir(executable), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s|%s|%s' \"$NATIVE_FIXTURE_SETTING\" \"$PWD\" \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "native")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	got, err := resolveNativeLaunch("native")
	if err != nil || got.Path != link || got.Resolved != executable || got.Installation != "npm-compatible" {
		t.Fatalf("install %+v %v", got, err)
	}
	// An arbitrary owner wrapper is deliberately NOT replaced by its child.
	wrapper := filepath.Join(dir, "wrapped")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexport NATIVE_FIXTURE_SETTING=owner-setting\nexec \"$(dirname \"$0\")/native\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err = resolveNativeLaunch("wrapped")
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	cmd := exec.Command(got.Path, "argument with spaces")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "owner-setting|"+cwd+"|argument with spaces" {
		t.Fatalf("native setup/arguments lost: %q", out)
	}
}

func TestOMPCapabilityTimeoutIsNotUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "omp")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := checkOMPLaunch(ctx, &Responder{Harness: "omp", Dir: dir})
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), "capabilities are unknown") {
		t.Fatalf("timeout misdiagnosed: %v", err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' '--print --no-session --extension'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := checkOMPLaunch(context.Background(), &Responder{Harness: "omp", Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' '--print'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := checkOMPLaunch(context.Background(), &Responder{Harness: "omp", Dir: dir}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("missing capability not diagnosed: %v", err)
	}
}

package services

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExecutableDirFromPathHandlesExtensionlessExecutable(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "code-switch-R")

	if err := os.WriteFile(exePath, []byte("test"), 0o755); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}

	got, err := executableDirFromPath(exePath)
	if err != nil {
		t.Fatalf("executableDirFromPath() error = %v", err)
	}
	if got != dir {
		t.Fatalf("executableDirFromPath() = %q, want %q", got, dir)
	}
}

func TestExecutableDirFromPathKeepsDirectory(t *testing.T) {
	dir := t.TempDir()

	got, err := executableDirFromPath(dir)
	if err != nil {
		t.Fatalf("executableDirFromPath() error = %v", err)
	}
	if got != dir {
		t.Fatalf("executableDirFromPath() = %q, want %q", got, dir)
	}
}

func TestGetAppConfigDirUsesUserHomeOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only app config directory contract")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := getAppConfigDir()
	if err != nil {
		t.Fatalf("getAppConfigDir() error = %v", err)
	}

	want := filepath.Join(home, projectAppConfigDirName)
	if got != want {
		t.Fatalf("getAppConfigDir() = %q, want %q", got, want)
	}
}

package download

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/auth"
)

func TestFindExecutables_Mac(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-mac-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a mock .app bundle
	appPath := filepath.Join(tmpDir, "TestGame.app")
	contentsPath := filepath.Join(appPath, "Contents")
	macOSPath := filepath.Join(contentsPath, "MacOS")

	if err := os.MkdirAll(macOSPath, 0o755); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

	// Create Info.plist
	plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>TestGame</string>
</dict>
</plist>`

	if err := os.WriteFile(filepath.Join(contentsPath, "Info.plist"), []byte(plistContent), 0o644); err != nil {
		t.Fatalf("failed to write Info.plist: %v", err)
	}

	// Create mock executable
	execPath := filepath.Join(macOSPath, "TestGame")
	if err := os.WriteFile(execPath, []byte("#!/bin/bash\necho test"), 0o755); err != nil {
		t.Fatalf("failed to create executable: %v", err)
	}

	// Find executables
	exes, err := FindExecutables(tmpDir, auth.BuildOSMac)
	if err != nil {
		t.Fatalf("FindExecutables failed: %v", err)
	}

	if len(exes) != 1 {
		t.Errorf("expected 1 executable, got %d", len(exes))
	}

	if len(exes) > 0 {
		if exes[0].Path != execPath {
			t.Errorf("expected path %q, got %q", execPath, exes[0].Path)
		}
		if exes[0].Name != "TestGame.app" {
			t.Errorf("expected name 'TestGame.app', got %q", exes[0].Name)
		}
	}
}

func TestFindExecutables_Windows(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-win-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create mock .exe files
	gameExe := filepath.Join(tmpDir, "Game.exe")
	launcherExe := filepath.Join(tmpDir, "Launcher.exe")
	uninstallExe := filepath.Join(tmpDir, "unins000.exe")

	for _, exe := range []string{gameExe, launcherExe, uninstallExe} {
		if err := os.WriteFile(exe, []byte("mock exe"), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", exe, err)
		}
	}

	// Find executables
	exes, err := FindExecutables(tmpDir, auth.BuildOSWindows)
	if err != nil {
		t.Fatalf("FindExecutables failed: %v", err)
	}

	// Should find Game.exe and Launcher.exe, but not unins000.exe
	if len(exes) != 2 {
		t.Errorf("expected 2 executables (excluding uninstaller), got %d", len(exes))
		for _, e := range exes {
			t.Logf("  - %s", e.Name)
		}
	}

	// Verify uninstaller is not included
	for _, e := range exes {
		if filepath.Base(e.Path) == "unins000.exe" {
			t.Error("unins000.exe should be excluded")
		}
	}
}

func TestFindExecutables_Linux(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-linux-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create mock executable files
	gameExe := filepath.Join(tmpDir, "game")
	launcherExe := filepath.Join(tmpDir, "launcher")
	scriptFile := filepath.Join(tmpDir, "start.sh")
	nonExec := filepath.Join(tmpDir, "readme.txt")

	// Create executables
	for _, exe := range []string{gameExe, launcherExe} {
		if err := os.WriteFile(exe, []byte("#!/bin/bash\necho test"), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", exe, err)
		}
	}

	// Create shell script (should be ignored)
	if err := os.WriteFile(scriptFile, []byte("#!/bin/bash\necho test"), 0o755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	// Create non-executable file
	if err := os.WriteFile(nonExec, []byte("readme"), 0o644); err != nil {
		t.Fatalf("failed to create non-exec: %v", err)
	}

	// Find executables
	exes, err := FindExecutables(tmpDir, auth.BuildOSLinux)
	if err != nil {
		t.Fatalf("FindExecutables failed: %v", err)
	}

	// Should find game and launcher, but not .sh or non-executable
	if len(exes) != 2 {
		t.Errorf("expected 2 executables, got %d", len(exes))
		for _, e := range exes {
			t.Logf("  - %s", e.Name)
		}
	}
}

func TestSelectExecutable_Single(t *testing.T) {
	exes := []Executable{
		{Path: "/path/to/game.exe", Name: "game.exe"},
	}

	selected, err := SelectExecutable(exes, "")
	if err != nil {
		t.Fatalf("SelectExecutable failed: %v", err)
	}

	if selected.Name != "game.exe" {
		t.Errorf("expected 'game.exe', got %q", selected.Name)
	}
}

func TestSelectExecutable_Multiple_WithName(t *testing.T) {
	exes := []Executable{
		{Path: "/path/to/game.exe", Name: "game.exe"},
		{Path: "/path/to/launcher.exe", Name: "launcher.exe"},
	}

	selected, err := SelectExecutable(exes, "launcher")
	if err != nil {
		t.Fatalf("SelectExecutable failed: %v", err)
	}

	if selected.Name != "launcher.exe" {
		t.Errorf("expected 'launcher.exe', got %q", selected.Name)
	}
}

func TestSelectExecutable_Multiple_NoName(t *testing.T) {
	exes := []Executable{
		{Path: "/path/to/game.exe", Name: "game.exe"},
		{Path: "/path/to/launcher.exe", Name: "launcher.exe"},
	}

	_, err := SelectExecutable(exes, "")
	if err == nil {
		t.Error("expected error for multiple executables without name")
	}
}

func TestSelectExecutable_Empty(t *testing.T) {
	exes := []Executable{}

	_, err := SelectExecutable(exes, "")
	if err == nil {
		t.Error("expected error for empty executables")
	}
}

func TestSelectExecutable_NotFound(t *testing.T) {
	exes := []Executable{
		{Path: "/path/to/game.exe", Name: "game.exe"},
	}

	_, err := SelectExecutable(exes, "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent executable")
	}
}

func TestIsIgnoredExecutable(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"unins000.exe", true},
		{"uninstall.exe", true},
		{"crashhandler.exe", true},
		{"vcredist_x64.exe", true},
		{"Game.exe", false},
		{"launcher.exe", false},
		{"MyGame.exe", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIgnoredExecutable(tt.name)
			if got != tt.expected {
				t.Errorf("isIgnoredExecutable(%q) = %v, expected %v", tt.name, got, tt.expected)
			}
		})
	}
}

func TestFindWine(t *testing.T) {
	// This test just verifies findWine doesn't panic
	// The actual result depends on the system
	_ = findWine()
}

func TestLaunchOptions(t *testing.T) {
	opts := &LaunchOptions{
		WinePath:   "/custom/wine",
		WinePrefix: "/home/user/.wine-game",
		NoWine:     false,
	}

	if opts.WinePath != "/custom/wine" {
		t.Errorf("expected WinePath '/custom/wine', got %q", opts.WinePath)
	}
	if opts.WinePrefix != "/home/user/.wine-game" {
		t.Errorf("expected WinePrefix '/home/user/.wine-game', got %q", opts.WinePrefix)
	}
}

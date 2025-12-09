package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gustash/freecarnival/auth"
)

func TestFindExecutables_Mac(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-mac-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	appPath := filepath.Join(tmpDir, "TestGame.app")
	contentsPath := filepath.Join(appPath, "Contents")
	macOSPath := filepath.Join(contentsPath, "MacOS")

	if err := os.MkdirAll(macOSPath, 0o755); err != nil {
		t.Fatalf("failed to create directories: %v", err)
	}

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

	execPath := filepath.Join(macOSPath, "TestGame")
	if err := os.WriteFile(execPath, []byte("#!/bin/bash\necho test"), 0o755); err != nil {
		t.Fatalf("failed to create executable: %v", err)
	}

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

	gameExe := filepath.Join(tmpDir, "Game.exe")
	launcherExe := filepath.Join(tmpDir, "Launcher.exe")
	uninstallExe := filepath.Join(tmpDir, "unins000.exe")

	for _, exe := range []string{gameExe, launcherExe, uninstallExe} {
		if err := os.WriteFile(exe, []byte("mock exe"), 0o644); err != nil {
			t.Fatalf("failed to create %s: %v", exe, err)
		}
	}

	exes, err := FindExecutables(tmpDir, auth.BuildOSWindows)
	if err != nil {
		t.Fatalf("FindExecutables failed: %v", err)
	}

	if len(exes) != 2 {
		t.Errorf("expected 2 executables (excluding uninstaller), got %d", len(exes))
		for _, e := range exes {
			t.Logf("  - %s", e.Name)
		}
	}

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

	gameExe := filepath.Join(tmpDir, "game")
	launcherExe := filepath.Join(tmpDir, "launcher")
	scriptFile := filepath.Join(tmpDir, "start.sh")
	nonExec := filepath.Join(tmpDir, "readme.txt")

	for _, exe := range []string{gameExe, launcherExe} {
		if err := os.WriteFile(exe, []byte("#!/bin/bash\necho test"), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", exe, err)
		}
	}

	if err := os.WriteFile(scriptFile, []byte("#!/bin/bash\necho test"), 0o755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	if err := os.WriteFile(nonExec, []byte("readme"), 0o644); err != nil {
		t.Fatalf("failed to create non-exec: %v", err)
	}

	exes, err := FindExecutables(tmpDir, auth.BuildOSLinux)
	if err != nil {
		t.Fatalf("FindExecutables failed: %v", err)
	}

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

func TestFindWineInCandidates_Found(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wine-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a fake wine executable
	fakeWinePath := filepath.Join(tmpDir, "wine")
	if err := os.WriteFile(fakeWinePath, []byte("#!/bin/bash\necho fake wine"), 0o755); err != nil {
		t.Fatalf("failed to create fake wine: %v", err)
	}

	// Create another fake candidate that doesn't exist
	nonExistentPath := filepath.Join(tmpDir, "nonexistent", "wine")

	// Test with custom candidates
	candidates := []string{
		nonExistentPath, // This one doesn't exist
		fakeWinePath,    // This one exists
	}

	winePath := findWineInCandidates(candidates)
	if winePath != fakeWinePath {
		t.Errorf("expected to find wine at %q, got %q", fakeWinePath, winePath)
	}
}

func TestFindWineInCandidates_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wine-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test with candidates that don't exist
	candidates := []string{
		filepath.Join(tmpDir, "nonexistent1", "wine"),
		filepath.Join(tmpDir, "nonexistent2", "wine"),
	}

	winePath := findWineInCandidates(candidates)
	if winePath != "" {
		t.Errorf("expected empty string when wine not found, got %q", winePath)
	}
}

func TestFindWineInCandidates_EmptyCandidates(t *testing.T) {
	// Test with no candidates
	winePath := findWineInCandidates([]string{})
	if winePath != "" {
		t.Errorf("expected empty string with no candidates, got %q", winePath)
	}
}

func TestFindWineInCandidates_FirstMatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wine-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create two fake wine executables
	wine1 := filepath.Join(tmpDir, "wine1")
	wine2 := filepath.Join(tmpDir, "wine2")

	if err := os.WriteFile(wine1, []byte("#!/bin/bash\necho wine1"), 0o755); err != nil {
		t.Fatalf("failed to create wine1: %v", err)
	}
	if err := os.WriteFile(wine2, []byte("#!/bin/bash\necho wine2"), 0o755); err != nil {
		t.Fatalf("failed to create wine2: %v", err)
	}

	// Should return the first one found
	candidates := []string{wine1, wine2}
	winePath := findWineInCandidates(candidates)

	if winePath != wine1 {
		t.Errorf("expected first match %q, got %q", wine1, winePath)
	}
}

func TestFindWine_Integration(t *testing.T) {
	// Integration test - just verify it doesn't panic and returns a valid result
	winePath := findWine()

	if winePath != "" {
		// If wine was found, verify the path exists
		if _, err := os.Stat(winePath); os.IsNotExist(err) {
			t.Errorf("findWine returned non-existent path: %q", winePath)
		}
		t.Logf("Wine found at: %s", winePath)
	} else {
		t.Log("Wine not found (expected if not installed)")
	}
}

func TestGame_NativeExecutable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a simple test script that exits immediately
	scriptContent := ""
	scriptName := "test_game"
	if runtime.GOOS == "windows" {
		scriptName = "test_game.bat"
		scriptContent = "@echo off\nexit /b 0"
	} else {
		scriptContent = "#!/bin/sh\nexit 0"
	}

	scriptPath := filepath.Join(tmpDir, scriptName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	ctx := context.Background()
	buildOS := auth.BuildOSLinux
	switch runtime.GOOS {
	case "darwin":
		buildOS = auth.BuildOSMac
	case "windows":
		buildOS = auth.BuildOSWindows
	}

	err = Game(ctx, scriptPath, buildOS, nil, nil)
	if err != nil {
		t.Errorf("Game() failed: %v", err)
	}
}

func TestGame_WithArguments(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a script that expects arguments
	scriptContent := ""
	scriptName := "test_game"
	if runtime.GOOS == "windows" {
		scriptName = "test_game.bat"
		scriptContent = "@echo off\nif \"%1\" == \"--test\" exit /b 0\nexit /b 1"
	} else {
		scriptContent = "#!/bin/sh\nif [ \"$1\" = \"--test\" ]; then exit 0; else exit 1; fi"
	}

	scriptPath := filepath.Join(tmpDir, scriptName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	ctx := context.Background()
	buildOS := auth.BuildOSLinux
	switch runtime.GOOS {
	case "darwin":
		buildOS = auth.BuildOSMac
	case "windows":
		buildOS = auth.BuildOSWindows
	}

	args := []string{"--test"}
	err = Game(ctx, scriptPath, buildOS, args, nil)
	if err != nil {
		t.Errorf("Game() with args failed: %v", err)
	}
}

func TestGame_ContextCancellation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a script with an infinite loop
	scriptContent := ""
	scriptName := "test_game"
	if runtime.GOOS == "windows" {
		scriptName = "test_game.bat"
		scriptContent = "@echo off\n:loop\ngoto loop"
	} else {
		scriptContent = "#!/bin/sh\nwhile true; do sleep 1; done"
	}

	scriptPath := filepath.Join(tmpDir, scriptName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	buildOS := auth.BuildOSLinux
	switch runtime.GOOS {
	case "darwin":
		buildOS = auth.BuildOSMac
	case "windows":
		buildOS = auth.BuildOSWindows
	}

	// Launch in goroutine and cancel after a short delay
	done := make(chan error, 1)
	go func() {
		done <- Game(ctx, scriptPath, buildOS, nil, nil)
	}()

	// Wait a bit then cancel
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Should return context.Canceled
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("process did not terminate after context cancellation")
	}

	// Verify process is actually dead
	time.Sleep(100 * time.Millisecond) // Give it a moment to clean up
	if isProcessRunning(scriptName) {
		t.Error("process is still running after cancellation")
	}
}

// isProcessRunning checks if a process with the given name is running
func isProcessRunning(name string) bool {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/FI", "IMAGENAME eq "+name)
	} else {
		cmd = exec.Command("pgrep", "-f", name)
	}

	output, err := cmd.Output()
	if err != nil {
		// pgrep returns exit code 1 when no processes found
		return false
	}

	// For Windows, check if the output contains the process name
	if runtime.GOOS == "windows" {
		return strings.Contains(string(output), name)
	}

	// For Unix, pgrep returns PIDs if found
	return len(strings.TrimSpace(string(output))) > 0
}

func TestGame_MissingExecutable(t *testing.T) {
	ctx := context.Background()
	nonExistentPath := "/tmp/nonexistent/game.exe"

	err := Game(ctx, nonExistentPath, auth.BuildOSLinux, nil, nil)
	if err == nil {
		t.Error("expected error for missing executable")
	}
}

func TestGame_WithWine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Wine test not applicable on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a fake wine executable
	fakeWinePath := filepath.Join(tmpDir, "wine")
	wineContent := "#!/bin/sh\nexit 0"
	if err := os.WriteFile(fakeWinePath, []byte(wineContent), 0o755); err != nil {
		t.Fatalf("failed to create fake wine: %v", err)
	}

	// Create a fake Windows executable
	gameExePath := filepath.Join(tmpDir, "game.exe")
	if err := os.WriteFile(gameExePath, []byte("fake exe"), 0o644); err != nil {
		t.Fatalf("failed to create fake exe: %v", err)
	}

	ctx := context.Background()
	opts := &Options{
		WinePath: fakeWinePath,
	}

	err = Game(ctx, gameExePath, auth.BuildOSWindows, nil, opts)
	if err != nil {
		t.Errorf("Game() with Wine failed: %v", err)
	}
}

func TestGame_NoWineFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Wine test not applicable on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a fake Windows executable
	gameExePath := filepath.Join(tmpDir, "game.exe")
	exeContent := "#!/bin/sh\nexit 0"
	if err := os.WriteFile(gameExePath, []byte(exeContent), 0o755); err != nil {
		t.Fatalf("failed to create fake exe: %v", err)
	}

	ctx := context.Background()
	opts := &Options{
		NoWine: true, // Disable Wine
	}

	// Should launch directly without Wine
	err = Game(ctx, gameExePath, auth.BuildOSWindows, nil, opts)
	if err != nil {
		t.Errorf("Game() with NoWine failed: %v", err)
	}
}

func TestGame_MacAppBundle(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a .app bundle structure
	appPath := filepath.Join(tmpDir, "TestGame.app")
	macOSPath := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macOSPath, 0o755); err != nil {
		t.Fatalf("failed to create app bundle: %v", err)
	}

	// Create executable inside bundle
	execPath := filepath.Join(macOSPath, "TestGame")
	execContent := "#!/bin/sh\nexit 0"
	if err := os.WriteFile(execPath, []byte(execContent), 0o755); err != nil {
		t.Fatalf("failed to create executable: %v", err)
	}

	ctx := context.Background()
	err = Game(ctx, execPath, auth.BuildOSMac, nil, nil)
	if err != nil {
		t.Errorf("Game() for macOS app failed: %v", err)
	}
}

func TestLaunchProcess_Success(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptContent := ""
	scriptName := "test_script"
	if runtime.GOOS == "windows" {
		scriptName = "test_script.bat"
		scriptContent = "@echo off\nexit /b 0"
	} else {
		scriptContent = "#!/bin/sh\nexit 0"
	}

	scriptPath := filepath.Join(tmpDir, scriptName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	ctx := context.Background()
	err = launchNative(ctx, scriptPath, nil)
	if err != nil {
		t.Errorf("launchNative() failed: %v", err)
	}
}

func TestLaunchProcess_NonZeroExit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "launch-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	scriptContent := ""
	scriptName := "test_script"
	if runtime.GOOS == "windows" {
		scriptName = "test_script.bat"
		scriptContent = "@echo off\nexit /b 42"
	} else {
		scriptContent = "#!/bin/sh\nexit 42"
	}

	scriptPath := filepath.Join(tmpDir, scriptName)
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("failed to create script: %v", err)
	}

	ctx := context.Background()
	err = launchNative(ctx, scriptPath, nil)
	if err == nil {
		t.Error("expected error for non-zero exit code")
	}
}

package launch

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

// Mac-specific tests

func TestFindMacAppBundles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mac-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	appPath := filepath.Join(tmpDir, "TestApp.app")
	contentsPath := filepath.Join(appPath, "Contents")
	macOSPath := filepath.Join(contentsPath, "MacOS")

	if err := os.MkdirAll(macOSPath, 0o755); err != nil {
		t.Fatalf("failed to create MacOS dir: %v", err)
	}

	infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>TestApp</string>
	<key>CFBundleName</key>
	<string>Test Application</string>
</dict>
</plist>`

	infoPlistPath := filepath.Join(contentsPath, "Info.plist")
	if err := os.WriteFile(infoPlistPath, []byte(infoPlist), 0o644); err != nil {
		t.Fatalf("failed to write Info.plist: %v", err)
	}

	executablePath := filepath.Join(macOSPath, "TestApp")
	if err := os.WriteFile(executablePath, []byte("#!/bin/bash\necho hello"), 0o644); err != nil {
		t.Fatalf("failed to create mock executable: %v", err)
	}

	bundles, err := FindMacAppBundles(tmpDir)
	if err != nil {
		t.Fatalf("FindMacAppBundles failed: %v", err)
	}

	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle, got %d", len(bundles))
	}

	bundle := bundles[0]
	if bundle.AppPath != appPath {
		t.Errorf("expected AppPath %q, got %q", appPath, bundle.AppPath)
	}
	if bundle.InfoPlistPath != infoPlistPath {
		t.Errorf("expected InfoPlistPath %q, got %q", infoPlistPath, bundle.InfoPlistPath)
	}
	if bundle.ExecutablePath != executablePath {
		t.Errorf("expected ExecutablePath %q, got %q", executablePath, bundle.ExecutablePath)
	}
}

func TestMacAppBundle_MarkAsExecutable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mac-exec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	executablePath := filepath.Join(tmpDir, "TestApp")
	if err := os.WriteFile(executablePath, []byte("#!/bin/bash\necho hello"), 0o644); err != nil {
		t.Fatalf("failed to create mock executable: %v", err)
	}

	info, err := os.Stat(executablePath)
	if err != nil {
		t.Fatalf("failed to stat executable: %v", err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Error("executable should not be executable initially")
	}

	bundle := &MacAppBundle{
		AppPath:        tmpDir,
		ExecutablePath: executablePath,
	}

	if err := bundle.MarkAsExecutable(); err != nil {
		t.Fatalf("MarkAsExecutable failed: %v", err)
	}

	info, err = os.Stat(executablePath)
	if err != nil {
		t.Fatalf("failed to stat executable after chmod: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected permissions 0755, got %o", info.Mode().Perm())
	}
}

func TestMacAppBundle_MarkAsExecutable_NoExecutable(t *testing.T) {
	bundle := &MacAppBundle{
		AppPath:        "/tmp/test",
		ExecutablePath: "",
	}

	err := bundle.MarkAsExecutable()
	if err == nil {
		t.Error("expected error for empty executable path")
	}
}

func TestMacAppBundle_MarkAsExecutable_NonExistent(t *testing.T) {
	bundle := &MacAppBundle{
		AppPath:        "/tmp/test",
		ExecutablePath: "/nonexistent/path/to/executable",
	}

	err := bundle.MarkAsExecutable()
	if err == nil {
		t.Error("expected error for non-existent executable")
	}
}

func TestParseInfoPlist(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "plist-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>MyExecutable</string>
</dict>
</plist>`

	plistPath := filepath.Join(tmpDir, "Info.plist")
	if err := os.WriteFile(plistPath, []byte(infoPlist), 0o644); err != nil {
		t.Fatalf("failed to write Info.plist: %v", err)
	}

	execName, err := parseInfoPlist(plistPath)
	if err != nil {
		t.Fatalf("parseInfoPlist failed: %v", err)
	}

	if execName != "MyExecutable" {
		t.Errorf("expected 'MyExecutable', got %q", execName)
	}
}

func TestParseInfoPlist_InvalidPlist(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "plist-invalid-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	plistPath := filepath.Join(tmpDir, "Info.plist")
	if err := os.WriteFile(plistPath, []byte("not a valid plist"), 0o644); err != nil {
		t.Fatalf("failed to write Info.plist: %v", err)
	}

	_, err = parseInfoPlist(plistPath)
	if err == nil {
		t.Error("expected error for invalid plist")
	}
}

func TestParseInfoPlist_NonExistent(t *testing.T) {
	_, err := parseInfoPlist("/nonexistent/Info.plist")
	if err == nil {
		t.Error("expected error for non-existent plist")
	}
}

func TestMarkMacExecutables(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "mark-mac-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	appPath := filepath.Join(tmpDir, "Game.app")
	contentsPath := filepath.Join(appPath, "Contents")
	macOSPath := filepath.Join(contentsPath, "MacOS")

	if err := os.MkdirAll(macOSPath, 0o755); err != nil {
		t.Fatalf("failed to create MacOS dir: %v", err)
	}

	infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>GameLauncher</string>
</dict>
</plist>`

	if err := os.WriteFile(filepath.Join(contentsPath, "Info.plist"), []byte(infoPlist), 0o644); err != nil {
		t.Fatalf("failed to write Info.plist: %v", err)
	}

	executablePath := filepath.Join(macOSPath, "GameLauncher")
	if err := os.WriteFile(executablePath, []byte("#!/bin/bash\necho game"), 0o644); err != nil {
		t.Fatalf("failed to create mock executable: %v", err)
	}

	if err := MarkMacExecutables(tmpDir); err != nil {
		t.Fatalf("MarkMacExecutables failed: %v", err)
	}

	info, err := os.Stat(executablePath)
	if err != nil {
		t.Fatalf("failed to stat executable: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("expected permissions 0755, got %o", info.Mode().Perm())
	}
}

func TestMarkMacExecutables_NoApps(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "no-apps-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.MkdirAll(filepath.Join(tmpDir, "SomeFolder"), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	if err := MarkMacExecutables(tmpDir); err != nil {
		t.Errorf("MarkMacExecutables should succeed with no apps: %v", err)
	}
}

func TestFindMacAppBundles_MultipleApps(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "multi-app-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, appName := range []string{"App1.app", "App2.app"} {
		appPath := filepath.Join(tmpDir, appName)
		contentsPath := filepath.Join(appPath, "Contents")
		macOSPath := filepath.Join(contentsPath, "MacOS")

		if err := os.MkdirAll(macOSPath, 0o755); err != nil {
			t.Fatalf("failed to create MacOS dir: %v", err)
		}

		execName := appName[:len(appName)-4]
		infoPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>` + execName + `</string>
</dict>
</plist>`

		if err := os.WriteFile(filepath.Join(contentsPath, "Info.plist"), []byte(infoPlist), 0o644); err != nil {
			t.Fatalf("failed to write Info.plist: %v", err)
		}

		if err := os.WriteFile(filepath.Join(macOSPath, execName), []byte("#!/bin/bash"), 0o644); err != nil {
			t.Fatalf("failed to create executable: %v", err)
		}
	}

	bundles, err := FindMacAppBundles(tmpDir)
	if err != nil {
		t.Fatalf("FindMacAppBundles failed: %v", err)
	}

	if len(bundles) != 2 {
		t.Errorf("expected 2 bundles, got %d", len(bundles))
	}
}

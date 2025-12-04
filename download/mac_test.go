package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindMacAppBundles(t *testing.T) {
	// Create a temporary directory structure with a mock .app bundle
	tmpDir, err := os.MkdirTemp("", "mac-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a mock .app bundle structure
	appPath := filepath.Join(tmpDir, "TestApp.app")
	contentsPath := filepath.Join(appPath, "Contents")
	macOSPath := filepath.Join(contentsPath, "MacOS")

	if err := os.MkdirAll(macOSPath, 0o755); err != nil {
		t.Fatalf("failed to create MacOS dir: %v", err)
	}

	// Create Info.plist
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

	// Create mock executable
	executablePath := filepath.Join(macOSPath, "TestApp")
	if err := os.WriteFile(executablePath, []byte("#!/bin/bash\necho hello"), 0o644); err != nil {
		t.Fatalf("failed to create mock executable: %v", err)
	}

	// Test finding the bundle
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
	// Create a temporary directory with a mock executable
	tmpDir, err := os.MkdirTemp("", "mac-exec-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create mock executable
	executablePath := filepath.Join(tmpDir, "TestApp")
	if err := os.WriteFile(executablePath, []byte("#!/bin/bash\necho hello"), 0o644); err != nil {
		t.Fatalf("failed to create mock executable: %v", err)
	}

	// Verify initial permissions are not executable
	info, err := os.Stat(executablePath)
	if err != nil {
		t.Fatalf("failed to stat executable: %v", err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Error("executable should not be executable initially")
	}

	// Mark as executable
	bundle := &MacAppBundle{
		AppPath:        tmpDir,
		ExecutablePath: executablePath,
	}

	if err := bundle.MarkAsExecutable(); err != nil {
		t.Fatalf("MarkAsExecutable failed: %v", err)
	}

	// Verify permissions are now executable
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

	// Create a valid Info.plist
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

	// Create invalid plist
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
	// Create a temporary directory with a mock .app bundle
	tmpDir, err := os.MkdirTemp("", "mark-mac-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create mock .app structure
	appPath := filepath.Join(tmpDir, "Game.app")
	contentsPath := filepath.Join(appPath, "Contents")
	macOSPath := filepath.Join(contentsPath, "MacOS")

	if err := os.MkdirAll(macOSPath, 0o755); err != nil {
		t.Fatalf("failed to create MacOS dir: %v", err)
	}

	// Create Info.plist
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

	// Create mock executable with non-executable permissions
	executablePath := filepath.Join(macOSPath, "GameLauncher")
	if err := os.WriteFile(executablePath, []byte("#!/bin/bash\necho game"), 0o644); err != nil {
		t.Fatalf("failed to create mock executable: %v", err)
	}

	// Run MarkMacExecutables
	if err := MarkMacExecutables(tmpDir); err != nil {
		t.Fatalf("MarkMacExecutables failed: %v", err)
	}

	// Verify executable permissions
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

	// Create a non-.app directory
	if err := os.MkdirAll(filepath.Join(tmpDir, "SomeFolder"), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Should succeed with no apps found
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

	// Create two .app bundles
	for _, appName := range []string{"App1.app", "App2.app"} {
		appPath := filepath.Join(tmpDir, appName)
		contentsPath := filepath.Join(appPath, "Contents")
		macOSPath := filepath.Join(contentsPath, "MacOS")

		if err := os.MkdirAll(macOSPath, 0o755); err != nil {
			t.Fatalf("failed to create MacOS dir: %v", err)
		}

		execName := appName[:len(appName)-4] // Remove .app
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

		// Create executable
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

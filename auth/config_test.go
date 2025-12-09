package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDir(t *testing.T) {
	// Test default behavior
	dir, err := configDir()
	if err != nil {
		t.Fatalf("configDir failed: %v", err)
	}

	if dir == "" {
		t.Error("configDir returned empty string")
	}

	if !filepath.IsAbs(dir) {
		t.Errorf("configDir should return absolute path, got %q", dir)
	}

	// Should contain FreeCarnival
	if filepath.Base(dir) != "FreeCarnival" {
		t.Errorf("expected directory name 'FreeCarnival', got %q", filepath.Base(dir))
	}
}

func TestConfigDir_WithTestOverride(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set test config directory
	SetTestConfigDir(tmpDir)
	defer SetTestConfigDir("") // Reset after test

	dir, err := configDir()
	if err != nil {
		t.Fatalf("configDir failed: %v", err)
	}

	if dir != tmpDir {
		t.Errorf("expected %q, got %q", tmpDir, dir)
	}
}

func TestSetTestConfigDir_Reset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set test config directory
	SetTestConfigDir(tmpDir)
	dir1, _ := configDir()

	// Reset to default
	SetTestConfigDir("")
	dir2, _ := configDir()

	if dir1 == dir2 {
		t.Error("configDir should return different path after reset")
	}

	if dir1 != tmpDir {
		t.Errorf("expected %q, got %q", tmpDir, dir1)
	}
}

func TestConfigFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-file-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	SetTestConfigDir(tmpDir)
	defer SetTestConfigDir("")

	filename := "test.json"
	path, err := configFile(filename)
	if err != nil {
		t.Fatalf("configFile failed: %v", err)
	}

	expectedPath := filepath.Join(tmpDir, filename)
	if path != expectedPath {
		t.Errorf("expected %q, got %q", expectedPath, path)
	}

	// Verify directory was created
	if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
		t.Error("configFile should create directory")
	}
}

func TestConfigFile_CreatesDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-file-create-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use a non-existent subdirectory
	configPath := filepath.Join(tmpDir, "subdir")
	SetTestConfigDir(configPath)
	defer SetTestConfigDir("")

	// This should create the directory
	_, err = configFile("test.json")
	if err != nil {
		t.Fatalf("configFile failed: %v", err)
	}

	// Verify directory was created with correct permissions
	info, err := os.Stat(configPath)
	if err != nil {
		t.Errorf("directory was not created: %v", err)
	}

	if !info.IsDir() {
		t.Error("configFile should create a directory")
	}

	if info.Mode().Perm() != 0o700 {
		t.Errorf("expected permissions 0700, got %o", info.Mode().Perm())
	}
}

func TestClearConfigFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clear-config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	SetTestConfigDir(tmpDir)
	defer SetTestConfigDir("")

	// Create a test file
	filename := "test.json"
	path, _ := configFile(filename)
	if err := os.WriteFile(path, []byte("test data"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("test file was not created")
	}

	// Clear the file
	if err := clearConfigFile(filename); err != nil {
		t.Fatalf("clearConfigFile failed: %v", err)
	}

	// Verify file is deleted
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}
}

func TestClearConfigFile_NonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "clear-config-nonexistent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	SetTestConfigDir(tmpDir)
	defer SetTestConfigDir("")

	// Try to clear non-existent file (should not error)
	if err := clearConfigFile("nonexistent.json"); err != nil {
		t.Errorf("clearConfigFile should not error for non-existent file, got: %v", err)
	}
}


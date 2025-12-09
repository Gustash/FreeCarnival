package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstalledConfig_SaveAndLoad(t *testing.T) {
	// Set up test directory
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	// Create test install info
	installed := InstalledConfig{
		"test-game": &InstallInfo{
			InstallPath: "/path/to/game",
			Version:     "1.0",
			OS:          BuildOSWindows,
		},
		"another-game": &InstallInfo{
			InstallPath: "/path/to/another",
			Version:     "2.0",
			OS:          BuildOSMac,
		},
	}

	// Save
	if err := SaveInstalled(installed); err != nil {
		t.Fatalf("SaveInstalled failed: %v", err)
	}

	// Load
	loaded, err := LoadInstalled()
	if err != nil {
		t.Fatalf("LoadInstalled failed: %v", err)
	}

	// Verify
	if len(loaded) != 2 {
		t.Errorf("expected 2 installed games, got %d", len(loaded))
	}

	game1 := loaded["test-game"]
	if game1 == nil {
		t.Fatal("test-game not found in loaded config")
	}
	if game1.InstallPath != "/path/to/game" {
		t.Errorf("InstallPath = %q, expected %q", game1.InstallPath, "/path/to/game")
	}
	if game1.Version != "1.0" {
		t.Errorf("Version = %q, expected %q", game1.Version, "1.0")
	}
	if game1.OS != BuildOSWindows {
		t.Errorf("OS = %q, expected %q", game1.OS, BuildOSWindows)
	}
}

func TestLoadInstalled_NotExists(t *testing.T) {
	// Set up test directory
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	// Load from empty config
	installed, err := LoadInstalled()
	if err != nil {
		t.Fatalf("LoadInstalled failed: %v", err)
	}

	// Should return empty map, not nil
	if installed == nil {
		t.Error("expected empty map, got nil")
	}
	if len(installed) != 0 {
		t.Errorf("expected empty map, got %d entries", len(installed))
	}
}

func TestAddInstalled(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	info := &InstallInfo{
		InstallPath: "/game/path",
		Version:     "1.0",
		OS:          BuildOSLinux,
	}

	// Add first game
	if err := AddInstalled("game1", info); err != nil {
		t.Fatalf("AddInstalled failed: %v", err)
	}

	// Add second game
	info2 := &InstallInfo{
		InstallPath: "/another/path",
		Version:     "2.0",
		OS:          BuildOSMac,
	}
	if err := AddInstalled("game2", info2); err != nil {
		t.Fatalf("AddInstalled failed: %v", err)
	}

	// Load and verify
	installed, err := LoadInstalled()
	if err != nil {
		t.Fatalf("LoadInstalled failed: %v", err)
	}

	if len(installed) != 2 {
		t.Errorf("expected 2 games, got %d", len(installed))
	}
}

func TestRemoveInstalled(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	// Add a game
	info := &InstallInfo{
		InstallPath: "/game/path",
		Version:     "1.0",
		OS:          BuildOSWindows,
	}
	if err := AddInstalled("game1", info); err != nil {
		t.Fatalf("AddInstalled failed: %v", err)
	}

	// Remove it
	if err := RemoveInstalled("game1"); err != nil {
		t.Fatalf("RemoveInstalled failed: %v", err)
	}

	// Verify it's gone
	installed, err := LoadInstalled()
	if err != nil {
		t.Fatalf("LoadInstalled failed: %v", err)
	}

	if len(installed) != 0 {
		t.Errorf("expected 0 games after removal, got %d", len(installed))
	}
}

func TestGetInstalled(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	// Add a game
	info := &InstallInfo{
		InstallPath: "/game/path",
		Version:     "1.0",
		OS:          BuildOSWindows,
	}
	if err := AddInstalled("game1", info); err != nil {
		t.Fatalf("AddInstalled failed: %v", err)
	}

	// Get existing game
	got, err := GetInstalled("game1")
	if err != nil {
		t.Fatalf("GetInstalled failed: %v", err)
	}
	if got == nil {
		t.Error("expected install info, got nil")
	}
	if got.Version != "1.0" {
		t.Errorf("Version = %q, expected %q", got.Version, "1.0")
	}

	// Get non-existing game
	got, err = GetInstalled("nonexistent")
	if err != nil {
		t.Fatalf("GetInstalled failed: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent game")
	}
}

func TestSaveManifest_And_LoadManifest(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	manifestData := []byte("Size in Bytes,Chunks,SHA,Flags,File Name\n100,1,abc123,0,test.txt\n")

	// Save manifest
	if err := SaveManifest("test-game", "1.0", "manifest", manifestData); err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}

	// Load manifest
	loaded, err := LoadManifest("test-game", "1.0", "manifest")
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	if string(loaded) != string(manifestData) {
		t.Errorf("loaded manifest doesn't match saved manifest")
	}
}

func TestLoadManifest_NotFound(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	_, err := LoadManifest("nonexistent", "1.0", "manifest")
	if err == nil {
		t.Error("expected error for nonexistent manifest")
	}
}

func TestSaveManifest_CreatesDirectoryStructure(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	manifestData := []byte("test data")

	if err := SaveManifest("my-game", "2.0", "manifest_chunks", manifestData); err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}

	// Verify directory structure was created
	expectedPath := filepath.Join(testDir, "manifests", "my-game", "2.0", "manifest_chunks.csv")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected manifest file at %s", expectedPath)
	}
}

func TestRemoveManifests(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	// Save some manifests for a game
	if err := SaveManifest("test-game", "1.0", "manifest", []byte("manifest data")); err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}
	if err := SaveManifest("test-game", "1.0", "manifest_chunks", []byte("chunks data")); err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}

	// Verify manifests exist
	manifestDir := filepath.Join(testDir, "manifests", "test-game")
	if _, err := os.Stat(manifestDir); os.IsNotExist(err) {
		t.Fatalf("manifests directory should exist")
	}

	// Remove manifests
	if err := RemoveManifests("test-game"); err != nil {
		t.Fatalf("RemoveManifests failed: %v", err)
	}

	// Verify manifests are gone
	if _, err := os.Stat(manifestDir); !os.IsNotExist(err) {
		t.Error("manifests directory should be removed")
	}
}

func TestRemoveManifests_NotExists(t *testing.T) {
	testDir := t.TempDir()
	SetTestConfigDir(testDir)
	defer SetTestConfigDir("")

	// Removing manifests for a game that doesn't exist should not error
	if err := RemoveManifests("nonexistent-game"); err != nil {
		t.Errorf("RemoveManifests should not error for nonexistent game: %v", err)
	}
}

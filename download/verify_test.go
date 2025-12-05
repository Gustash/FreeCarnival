package download

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/auth"
)

func TestHashFile(t *testing.T) {
	// Create a temp file with known content
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Calculate expected hash
	hasher := sha256.New()
	hasher.Write(content)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	// Test hashFile
	actualHash, err := hashFile(filePath)
	if err != nil {
		t.Fatalf("hashFile failed: %v", err)
	}

	if actualHash != expectedHash {
		t.Errorf("hashFile() = %q, expected %q", actualHash, expectedHash)
	}
}

func TestHashFile_NotFound(t *testing.T) {
	_, err := hashFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestVerifyChunk(t *testing.T) {
	data := []byte("test chunk data")
	hasher := sha256.New()
	hasher.Write(data)
	expectedSHA := hex.EncodeToString(hasher.Sum(nil))

	if !VerifyChunk(data, expectedSHA) {
		t.Error("VerifyChunk should return true for matching hash")
	}

	if VerifyChunk(data, "wronghash") {
		t.Error("VerifyChunk should return false for non-matching hash")
	}
}

func TestVerifyFile_Success(t *testing.T) {
	// Create a temp directory with a test file
	dir := t.TempDir()
	content := []byte("file content for verification")
	filePath := filepath.Join(dir, "game", "test.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Calculate SHA
	hasher := sha256.New()
	hasher.Write(content)
	sha := hex.EncodeToString(hasher.Sum(nil))

	record := BuildManifestRecord{
		FileName:    filepath.Join("game", "test.txt"),
		SHA:         sha,
		SizeInBytes: len(content),
	}

	result := verifyFile(dir, record)

	if !result.Valid {
		t.Errorf("expected file to be valid, got error: %v", result.Error)
	}
	if result.Expected != sha {
		t.Errorf("Expected = %q, want %q", result.Expected, sha)
	}
	if result.Actual != sha {
		t.Errorf("Actual = %q, want %q", result.Actual, sha)
	}
}

func TestVerifyFile_Missing(t *testing.T) {
	dir := t.TempDir()

	record := BuildManifestRecord{
		FileName:    "missing.txt",
		SHA:         "abc123",
		SizeInBytes: 100,
	}

	result := verifyFile(dir, record)

	if result.Valid {
		t.Error("expected missing file to be invalid")
	}
	if result.Error == nil {
		t.Error("expected error for missing file")
	}
}

func TestVerifyFile_WrongSize(t *testing.T) {
	dir := t.TempDir()
	content := []byte("short")
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	record := BuildManifestRecord{
		FileName:    "test.txt",
		SHA:         "abc123",
		SizeInBytes: 1000, // Wrong size
	}

	result := verifyFile(dir, record)

	if result.Valid {
		t.Error("expected file with wrong size to be invalid")
	}
}

func TestVerifyFile_WrongHash(t *testing.T) {
	dir := t.TempDir()
	content := []byte("test content")
	filePath := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	record := BuildManifestRecord{
		FileName:    "test.txt",
		SHA:         "wronghash123456789",
		SizeInBytes: len(content),
	}

	result := verifyFile(dir, record)

	if result.Valid {
		t.Error("expected file with wrong hash to be invalid")
	}
}

func TestVerifyInstallation_Success(t *testing.T) {
	// Set up test config directory
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	// Create a test installation directory
	installDir := filepath.Join(testDir, "game")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("failed to create install dir: %v", err)
	}

	// Create test files
	content1 := []byte("file one content")
	content2 := []byte("file two content with more data")

	if err := os.WriteFile(filepath.Join(installDir, "file1.txt"), content1, 0o644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "file2.txt"), content2, 0o644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	// Calculate SHAs
	hasher1 := sha256.New()
	hasher1.Write(content1)
	sha1 := hex.EncodeToString(hasher1.Sum(nil))

	hasher2 := sha256.New()
	hasher2.Write(content2)
	sha2 := hex.EncodeToString(hasher2.Sum(nil))

	// Create manifest CSV
	manifestCSV := "Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag\n"
	manifestCSV += "16,1," + sha1 + ",0,file1.txt,\n"
	manifestCSV += "31,1," + sha2 + ",0,file2.txt,\n"

	// Save manifest
	if err := auth.SaveManifest("test-game", "1.0", "manifest", []byte(manifestCSV)); err != nil {
		t.Fatalf("failed to save manifest: %v", err)
	}

	// Create install info
	installInfo := &auth.InstallInfo{
		InstallPath: installDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	// Verify
	opts := VerifyOptions{Verbose: false}
	valid, results, err := VerifyInstallation("test-game", installInfo, opts)

	if err != nil {
		t.Fatalf("VerifyInstallation failed: %v", err)
	}
	if !valid {
		t.Error("expected verification to pass")
		for _, r := range results {
			if !r.Valid {
				t.Errorf("  %s: %v", r.FilePath, r.Error)
			}
		}
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestVerifyInstallation_CorruptedFile(t *testing.T) {
	// Set up test config directory
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	// Create a test installation directory
	installDir := filepath.Join(testDir, "game")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("failed to create install dir: %v", err)
	}

	// Create a corrupted file (content doesn't match manifest)
	if err := os.WriteFile(filepath.Join(installDir, "file.txt"), []byte("corrupted content"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create manifest with different hash
	manifestCSV := "Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag\n"
	manifestCSV += "17,1,wronghashvalue123456,0,file.txt,\n"

	// Save manifest
	if err := auth.SaveManifest("test-game", "1.0", "manifest", []byte(manifestCSV)); err != nil {
		t.Fatalf("failed to save manifest: %v", err)
	}

	// Create install info
	installInfo := &auth.InstallInfo{
		InstallPath: installDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	// Verify
	opts := VerifyOptions{Verbose: false}
	valid, results, err := VerifyInstallation("test-game", installInfo, opts)

	if err != nil {
		t.Fatalf("VerifyInstallation failed: %v", err)
	}
	if valid {
		t.Error("expected verification to fail for corrupted file")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestVerifyInstallation_MissingManifest(t *testing.T) {
	// Set up test config directory
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	installInfo := &auth.InstallInfo{
		InstallPath: testDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	opts := VerifyOptions{}
	_, _, err := VerifyInstallation("nonexistent-game", installInfo, opts)

	if err == nil {
		t.Error("expected error for missing manifest")
	}
}

func TestVerifyInstallation_SkipsDirectories(t *testing.T) {
	// Set up test config directory
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	// Create a test installation directory with a subdirectory
	installDir := filepath.Join(testDir, "game")
	subDir := filepath.Join(installDir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	// Create a file
	content := []byte("test")
	if err := os.WriteFile(filepath.Join(installDir, "file.txt"), content, 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(content)
	sha := hex.EncodeToString(hasher.Sum(nil))

	// Create manifest with both directory and file
	manifestCSV := "Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag\n"
	manifestCSV += "0,0,,40,subdir,\n" // Directory (Flags=40)
	manifestCSV += "4,1," + sha + ",0,file.txt,\n"

	// Save manifest
	if err := auth.SaveManifest("test-game", "1.0", "manifest", []byte(manifestCSV)); err != nil {
		t.Fatalf("failed to save manifest: %v", err)
	}

	installInfo := &auth.InstallInfo{
		InstallPath: installDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	opts := VerifyOptions{}
	valid, results, err := VerifyInstallation("test-game", installInfo, opts)

	if err != nil {
		t.Fatalf("VerifyInstallation failed: %v", err)
	}
	if !valid {
		t.Error("expected verification to pass")
	}
	// Should only have 1 result (file), not 2 (directory should be skipped)
	if len(results) != 1 {
		t.Errorf("expected 1 result (directory should be skipped), got %d", len(results))
	}
}

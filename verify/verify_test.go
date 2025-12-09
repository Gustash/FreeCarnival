package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/manifest"
)

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.txt")
	content := []byte("hello world")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(content)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	actualHash, err := HashFile(filePath)
	if err != nil {
		t.Fatalf("HashFile failed: %v", err)
	}

	if actualHash != expectedHash {
		t.Errorf("HashFile() = %q, expected %q", actualHash, expectedHash)
	}
}

func TestHashFile_NotFound(t *testing.T) {
	_, err := HashFile("/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestChunk(t *testing.T) {
	data := []byte("test chunk data")
	hasher := sha256.New()
	hasher.Write(data)
	expectedSHA := hex.EncodeToString(hasher.Sum(nil))

	if !Chunk(data, expectedSHA) {
		t.Error("Chunk should return true for matching hash")
	}

	if Chunk(data, "wronghash") {
		t.Error("Chunk should return false for non-matching hash")
	}
}

func TestVerifyFile_Success(t *testing.T) {
	dir := t.TempDir()
	content := []byte("file content for verification")
	filePath := filepath.Join(dir, "game", "test.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(content)
	sha := hex.EncodeToString(hasher.Sum(nil))

	record := manifest.BuildRecord{
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

	record := manifest.BuildRecord{
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

	record := manifest.BuildRecord{
		FileName:    "test.txt",
		SHA:         "abc123",
		SizeInBytes: 1000,
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

	record := manifest.BuildRecord{
		FileName:    "test.txt",
		SHA:         "wronghash123456789",
		SizeInBytes: len(content),
	}

	result := verifyFile(dir, record)

	if result.Valid {
		t.Error("expected file with wrong hash to be invalid")
	}
}

func TestInstallation_Success(t *testing.T) {
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	installDir := filepath.Join(testDir, "game")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("failed to create install dir: %v", err)
	}

	content1 := []byte("file one content")
	content2 := []byte("file two content with more data")

	if err := os.WriteFile(filepath.Join(installDir, "file1.txt"), content1, 0o644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "file2.txt"), content2, 0o644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	hasher1 := sha256.New()
	hasher1.Write(content1)
	sha1 := hex.EncodeToString(hasher1.Sum(nil))

	hasher2 := sha256.New()
	hasher2.Write(content2)
	sha2 := hex.EncodeToString(hasher2.Sum(nil))

	manifestCSV := "Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag\n"
	manifestCSV += "16,1," + sha1 + ",0,file1.txt,\n"
	manifestCSV += "31,1," + sha2 + ",0,file2.txt,\n"

	records, err := manifest.ParseBuildManifest([]byte(manifestCSV))
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	installInfo := &auth.InstallInfo{
		InstallPath: installDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	opts := Options{Verbose: false}
	valid, results, err := Installation(installInfo, records, opts)

	if err != nil {
		t.Fatalf("Installation failed: %v", err)
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

func TestInstallation_CorruptedFile(t *testing.T) {
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	installDir := filepath.Join(testDir, "game")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("failed to create install dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(installDir, "file.txt"), []byte("corrupted content"), 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	manifestCSV := "Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag\n"
	manifestCSV += "17,1,wronghashvalue123456,0,file.txt,\n"

	records, err := manifest.ParseBuildManifest([]byte(manifestCSV))
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	installInfo := &auth.InstallInfo{
		InstallPath: installDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	opts := Options{Verbose: false}
	valid, results, err := Installation(installInfo, records, opts)

	if err != nil {
		t.Fatalf("Installation failed: %v", err)
	}
	if valid {
		t.Error("expected verification to fail for corrupted file")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestInstallation_EmptyManifest(t *testing.T) {
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	installInfo := &auth.InstallInfo{
		InstallPath: testDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	opts := Options{}
	valid, results, err := Installation(installInfo, []manifest.BuildRecord{}, opts)

	if err != nil {
		t.Fatalf("Installation failed: %v", err)
	}
	if !valid {
		t.Error("expected empty manifest to be valid")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty manifest, got %d", len(results))
	}
}

func TestInstallation_SkipsDirectories(t *testing.T) {
	testDir := t.TempDir()
	auth.SetTestConfigDir(testDir)
	defer auth.SetTestConfigDir("")

	installDir := filepath.Join(testDir, "game")
	subDir := filepath.Join(installDir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}

	content := []byte("test")
	if err := os.WriteFile(filepath.Join(installDir, "file.txt"), content, 0o644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(content)
	sha := hex.EncodeToString(hasher.Sum(nil))

	manifestCSV := "Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag\n"
	manifestCSV += "0,0,,40,subdir,\n"
	manifestCSV += "4,1," + sha + ",0,file.txt,\n"

	records, err := manifest.ParseBuildManifest([]byte(manifestCSV))
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	installInfo := &auth.InstallInfo{
		InstallPath: installDir,
		Version:     "1.0",
		OS:          auth.BuildOSWindows,
	}

	opts := Options{}
	valid, results, err := Installation(installInfo, records, opts)

	if err != nil {
		t.Fatalf("Installation failed: %v", err)
	}
	if !valid {
		t.Error("expected verification to pass")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (directory should be skipped), got %d", len(results))
	}
}

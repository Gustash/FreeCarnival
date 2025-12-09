package update

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/download"
	"github.com/gustash/freecarnival/manifest"
)

func TestNew(t *testing.T) {
	client := &http.Client{}
	product := &auth.Product{
		Name:        "Test Game",
		SluggedName: "test-game",
	}
	oldVersion := &auth.ProductVersion{
		Version: "1.0.0",
		OS:      auth.BuildOSWindows,
	}
	newVersion := &auth.ProductVersion{
		Version: "2.0.0",
		OS:      auth.BuildOSWindows,
	}
	installPath := "/tmp/test"
	options := download.Options{
		MaxDownloadWorkers: 4,
		MaxMemoryUsage:     1024,
	}

	updater := New(client, product, oldVersion, newVersion, installPath, options)

	if updater == nil {
		t.Fatal("New returned nil")
	}
	if updater.client != client {
		t.Error("client not set correctly")
	}
	if updater.product != product {
		t.Error("product not set correctly")
	}
	if updater.oldVersion != oldVersion {
		t.Error("oldVersion not set correctly")
	}
	if updater.newVersion != newVersion {
		t.Error("newVersion not set correctly")
	}
	if updater.installPath != installPath {
		t.Errorf("expected installPath %q, got %q", installPath, updater.installPath)
	}
	if updater.options.MaxDownloadWorkers != options.MaxDownloadWorkers {
		t.Errorf("expected MaxDownloadWorkers %d, got %d", options.MaxDownloadWorkers, updater.options.MaxDownloadWorkers)
	}
	if updater.options.MaxMemoryUsage != options.MaxMemoryUsage {
		t.Errorf("expected MaxMemoryUsage %d, got %d", options.MaxMemoryUsage, updater.options.MaxMemoryUsage)
	}
}

func TestCheckForResumeUpdate_FreshUpdate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "resume-fresh-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	oldManifest := []manifest.BuildRecord{
		{FileName: "existing.txt", SizeInBytes: 100},
	}

	delta := &DeltaManifest{
		Added: []manifest.BuildRecord{
			{FileName: "new_file.txt", SizeInBytes: 50, ChangeTag: manifest.ChangeTagAdded},
		},
		Modified: []manifest.BuildRecord{
			{FileName: "existing.txt", SizeInBytes: 150, ChangeTag: manifest.ChangeTagModified},
		},
	}

	// Fresh update - no files from delta exist
	isResume := updater.checkForResumeUpdate(delta, oldManifest)
	if isResume {
		t.Error("expected false for fresh update (no added files exist)")
	}
}

func TestCheckForResumeUpdate_AddedFileExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "resume-added-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create an "added" file (indicating resume)
	addedFile := filepath.Join(tmpDir, "new_file.txt")
	if err := os.WriteFile(addedFile, []byte("partial content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	oldManifest := []manifest.BuildRecord{}
	delta := &DeltaManifest{
		Added: []manifest.BuildRecord{
			{FileName: "new_file.txt", SizeInBytes: 100, ChangeTag: manifest.ChangeTagAdded},
		},
	}

	isResume := updater.checkForResumeUpdate(delta, oldManifest)
	if !isResume {
		t.Error("expected true when added file exists (indicates resume)")
	}
}

func TestCheckForResumeUpdate_ModifiedFileDifferentSize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "resume-modified-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create a modified file with size different from old version
	modifiedFile := filepath.Join(tmpDir, "game.dat")
	newContent := []byte("new version partial content")
	if err := os.WriteFile(modifiedFile, newContent, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	oldManifest := []manifest.BuildRecord{
		{FileName: "game.dat", SizeInBytes: 50}, // Old size was 50
	}
	delta := &DeltaManifest{
		Modified: []manifest.BuildRecord{
			{FileName: "game.dat", SizeInBytes: 200, ChangeTag: manifest.ChangeTagModified},
		},
	}

	isResume := updater.checkForResumeUpdate(delta, oldManifest)
	if !isResume {
		t.Error("expected true when modified file has different size than old version")
	}
}

func TestCheckForResumeUpdate_ModifiedFileMatchesOldSize(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "resume-match-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create a modified file with SAME size as old version (not resumed)
	modifiedFile := filepath.Join(tmpDir, "game.dat")
	content := make([]byte, 100) // Exactly 100 bytes
	if err := os.WriteFile(modifiedFile, content, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	oldManifest := []manifest.BuildRecord{
		{FileName: "game.dat", SizeInBytes: 100}, // Same size
	}
	delta := &DeltaManifest{
		Modified: []manifest.BuildRecord{
			{FileName: "game.dat", SizeInBytes: 200, ChangeTag: manifest.ChangeTagModified},
		},
	}

	isResume := updater.checkForResumeUpdate(delta, oldManifest)
	if isResume {
		t.Error("expected false when modified file matches old size (fresh update)")
	}
}

func TestCheckForResumeUpdate_SkipsDirectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "resume-dirs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create directory that matches an "added" directory
	addedDir := filepath.Join(tmpDir, "new_folder")
	if err := os.MkdirAll(addedDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	oldManifest := []manifest.BuildRecord{}
	delta := &DeltaManifest{
		Added: []manifest.BuildRecord{
			{FileName: "new_folder", Flags: 40, ChangeTag: manifest.ChangeTagAdded}, // Directory
		},
	}

	isResume := updater.checkForResumeUpdate(delta, oldManifest)
	if isResume {
		t.Error("directories should be skipped in resume detection")
	}
}

func TestCheckForResumeUpdate_SkipsEmptyFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "resume-empty-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create an empty file
	emptyFile := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(emptyFile, []byte{}, 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	oldManifest := []manifest.BuildRecord{}
	delta := &DeltaManifest{
		Added: []manifest.BuildRecord{
			{FileName: "empty.txt", SizeInBytes: 0, ChangeTag: manifest.ChangeTagAdded}, // Empty file
		},
	}

	isResume := updater.checkForResumeUpdate(delta, oldManifest)
	if isResume {
		t.Error("empty files should be skipped in resume detection")
	}
}

func TestCheckForResumeUpdate_EmptyDelta(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "resume-empty-delta-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	isResume := updater.checkForResumeUpdate(&DeltaManifest{}, []manifest.BuildRecord{})
	if isResume {
		t.Error("empty delta should not be considered a resume")
	}
}

func TestCleanupFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup-files-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create test files
	modifiedFile := filepath.Join(tmpDir, "modified.txt")
	if err := os.WriteFile(modifiedFile, []byte("old content"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	removedFile := filepath.Join(tmpDir, "removed.txt")
	if err := os.WriteFile(removedFile, []byte("to be removed"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	removedDir := filepath.Join(tmpDir, "old_dir")
	if err := os.MkdirAll(removedDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	delta := &DeltaManifest{
		Modified: []manifest.BuildRecord{
			{FileName: "modified.txt", Flags: 0, ChangeTag: manifest.ChangeTagModified},
		},
		Removed: []manifest.BuildRecord{
			{FileName: "removed.txt", Flags: 0, ChangeTag: manifest.ChangeTagRemoved},
			{FileName: "old_dir", Flags: 40, ChangeTag: manifest.ChangeTagRemoved},
		},
	}

	if err := updater.cleanupFiles(delta); err != nil {
		t.Fatalf("cleanupFiles failed: %v", err)
	}

	// Verify modified file is removed
	if _, err := os.Stat(modifiedFile); !os.IsNotExist(err) {
		t.Error("modified file should be removed")
	}

	// Verify removed file is removed
	if _, err := os.Stat(removedFile); !os.IsNotExist(err) {
		t.Error("removed file should be removed")
	}

	// Verify removed directory is removed
	if _, err := os.Stat(removedDir); !os.IsNotExist(err) {
		t.Error("removed directory should be removed")
	}
}

func TestCleanupFiles_SkipsModifiedDirectories(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup-dirs-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create a directory
	modifiedDir := filepath.Join(tmpDir, "some_dir")
	if err := os.MkdirAll(modifiedDir, 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	delta := &DeltaManifest{
		Modified: []manifest.BuildRecord{
			{FileName: "some_dir", Flags: 40, ChangeTag: manifest.ChangeTagModified}, // Directory
		},
	}

	if err := updater.cleanupFiles(delta); err != nil {
		t.Fatalf("cleanupFiles failed: %v", err)
	}

	// Directory should NOT be removed (directories are skipped in modified cleanup)
	if _, err := os.Stat(modifiedDir); os.IsNotExist(err) {
		t.Error("modified directories should be skipped, not removed")
	}
}

func TestCleanupRemovedFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup-removed-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	// Create files and directories to remove
	file1 := filepath.Join(tmpDir, "old_file.txt")
	if err := os.WriteFile(file1, []byte("old"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	dir1 := filepath.Join(tmpDir, "old_dir")
	if err := os.MkdirAll(filepath.Join(dir1, "subdir"), 0o755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	nestedFile := filepath.Join(dir1, "nested.txt")
	if err := os.WriteFile(nestedFile, []byte("nested"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	delta := &DeltaManifest{
		Removed: []manifest.BuildRecord{
			{FileName: "old_file.txt", Flags: 0, ChangeTag: manifest.ChangeTagRemoved},
			{FileName: "old_dir", Flags: 40, ChangeTag: manifest.ChangeTagRemoved},
		},
	}

	if err := updater.cleanupRemovedFiles(delta); err != nil {
		t.Fatalf("cleanupRemovedFiles failed: %v", err)
	}

	if _, err := os.Stat(file1); !os.IsNotExist(err) {
		t.Error("old_file.txt should be removed")
	}

	if _, err := os.Stat(dir1); !os.IsNotExist(err) {
		t.Error("old_dir should be removed (including contents)")
	}
}

func TestCleanupRemovedFiles_NonExistentFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup-nonexistent-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	updater := &Updater{installPath: tmpDir}

	delta := &DeltaManifest{
		Removed: []manifest.BuildRecord{
			{FileName: "does_not_exist.txt", Flags: 0, ChangeTag: manifest.ChangeTagRemoved},
			{FileName: "missing_dir", Flags: 40, ChangeTag: manifest.ChangeTagRemoved},
		},
	}

	// Should not error when files don't exist
	if err := updater.cleanupRemovedFiles(delta); err != nil {
		t.Errorf("cleanupRemovedFiles should not error for non-existent files: %v", err)
	}
}

func TestCleanupRemovedFiles_EmptyDelta(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup-empty-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file that should NOT be touched
	keepFile := filepath.Join(tmpDir, "keep.txt")
	if err := os.WriteFile(keepFile, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	updater := &Updater{installPath: tmpDir}
	delta := &DeltaManifest{Removed: []manifest.BuildRecord{}}

	if err := updater.cleanupRemovedFiles(delta); err != nil {
		t.Fatalf("cleanupRemovedFiles failed: %v", err)
	}

	// File should still exist
	if _, err := os.Stat(keepFile); os.IsNotExist(err) {
		t.Error("unrelated files should not be touched")
	}
}

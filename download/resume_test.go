package download

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/manifest"
)

func TestResumeChecker_NewFile(t *testing.T) {
	tmpDir := t.TempDir()

	fileInfoMap := map[string]*FileInfo{
		"test.txt": {
			Index:      0,
			FullPath:   filepath.Join(tmpDir, "test.txt"),
			ChunkCount: 2,
			Record:     manifest.BuildRecord{SizeInBytes: 2 * manifest.MaxChunkSize, SHA: "abc123"},
		},
	}

	fileChunks := map[int][]manifest.ChunkRecord{
		0: {{ChunkSHA: "sha1"}, {ChunkSHA: "sha2"}},
	}

	checker := NewResumeChecker(fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	if state.StartChunkIndex[0] != 0 {
		t.Errorf("expected startChunk 0, got %d", state.StartChunkIndex[0])
	}
	if state.CompletedFiles[0] {
		t.Error("file should not be marked complete")
	}
	if state.BytesAlreadyDownloaded != 0 {
		t.Errorf("expected 0 bytes already downloaded, got %d", state.BytesAlreadyDownloaded)
	}
}

func TestResumeChecker_CompleteFile(t *testing.T) {
	tmpDir := t.TempDir()

	filePath := filepath.Join(tmpDir, "complete.txt")
	content := []byte("hello world")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(content)
	expectedSHA := hex.EncodeToString(hasher.Sum(nil))

	fileInfoMap := map[string]*FileInfo{
		"complete.txt": {
			Index:      0,
			FullPath:   filePath,
			ChunkCount: 1,
			Record:     manifest.BuildRecord{SizeInBytes: len(content), SHA: expectedSHA},
		},
	}

	fileChunks := map[int][]manifest.ChunkRecord{
		0: {{ChunkSHA: "sha1"}},
	}

	checker := NewResumeChecker(fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	if !state.CompletedFiles[0] {
		t.Error("file should be marked complete")
	}
	if state.BytesAlreadyDownloaded != int64(len(content)) {
		t.Errorf("expected %d bytes already downloaded, got %d", len(content), state.BytesAlreadyDownloaded)
	}
	if state.FilesAlreadyComplete != 1 {
		t.Errorf("expected 1 file complete, got %d", state.FilesAlreadyComplete)
	}
}

func TestResumeChecker_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()

	filePath := filepath.Join(tmpDir, "corrupted.txt")
	content := []byte("hello world")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	fileInfoMap := map[string]*FileInfo{
		"corrupted.txt": {
			Index:      0,
			FullPath:   filePath,
			ChunkCount: 1,
			Record:     manifest.BuildRecord{SizeInBytes: len(content), SHA: "wronghash"},
		},
	}

	fileChunks := map[int][]manifest.ChunkRecord{
		0: {{ChunkSHA: "sha1"}},
	}

	checker := NewResumeChecker(fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	if state.CompletedFiles[0] {
		t.Error("file should not be marked complete")
	}
	if !state.CorruptedFiles[0] {
		t.Error("file should be marked corrupted")
	}
}

func TestResumeChecker_PartialFile(t *testing.T) {
	tmpDir := t.TempDir()

	filePath := filepath.Join(tmpDir, "partial.txt")
	chunkData := make([]byte, manifest.MaxChunkSize)
	for i := range chunkData {
		chunkData[i] = byte(i % 256)
	}
	if err := os.WriteFile(filePath, chunkData, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	hasher := sha256.New()
	hasher.Write(chunkData)
	chunkSHA := hex.EncodeToString(hasher.Sum(nil))

	fileInfoMap := map[string]*FileInfo{
		"partial.txt": {
			Index:      0,
			FullPath:   filePath,
			ChunkCount: 3,
			Record:     manifest.BuildRecord{SizeInBytes: 3 * manifest.MaxChunkSize, SHA: "fullfilehash"},
		},
	}

	fileChunks := map[int][]manifest.ChunkRecord{
		0: {{ChunkSHA: chunkSHA}, {ChunkSHA: "chunk2"}, {ChunkSHA: "chunk3"}},
	}

	checker := NewResumeChecker(fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	if state.CompletedFiles[0] {
		t.Error("file should not be marked complete")
	}
	if state.CorruptedFiles[0] {
		t.Error("file should not be marked corrupted")
	}
	if state.StartChunkIndex[0] != 1 {
		t.Errorf("expected startChunk 1, got %d", state.StartChunkIndex[0])
	}
	if state.BytesAlreadyDownloaded != int64(manifest.MaxChunkSize) {
		t.Errorf("expected %d bytes already downloaded, got %d", manifest.MaxChunkSize, state.BytesAlreadyDownloaded)
	}
}

func TestFilterChunksToDownload(t *testing.T) {
	fileChunks := map[int][]manifest.ChunkRecord{
		0: {{ChunkSHA: "a"}, {ChunkSHA: "b"}, {ChunkSHA: "c"}},
		1: {{ChunkSHA: "d"}, {ChunkSHA: "e"}},
		2: {{ChunkSHA: "f"}},
	}

	state := &ResumeState{
		CompletedFiles:  map[int]bool{1: true},
		CorruptedFiles:  map[int]bool{2: true},
		StartChunkIndex: map[int]int{0: 1, 2: 0},
	}

	filtered := FilterChunksToDownload(fileChunks, state)

	if len(filtered[0]) != 2 {
		t.Errorf("file 0: expected 2 chunks, got %d", len(filtered[0]))
	}
	if filtered[0][0].ChunkSHA != "b" {
		t.Errorf("file 0: expected first chunk 'b', got %q", filtered[0][0].ChunkSHA)
	}

	if _, ok := filtered[1]; ok {
		t.Error("file 1 should be skipped (complete)")
	}

	if len(filtered[2]) != 1 {
		t.Errorf("file 2: expected 1 chunk, got %d", len(filtered[2]))
	}
}

func TestHasExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	fileInfoMap := map[string]*FileInfo{
		"test.txt": {FullPath: filepath.Join(tmpDir, "test.txt")},
	}
	if hasExistingFiles(fileInfoMap) {
		t.Error("expected no existing files")
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if !hasExistingFiles(fileInfoMap) {
		t.Error("expected existing files")
	}
}

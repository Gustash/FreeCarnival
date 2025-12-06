package download

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestResumeChecker_NewFile(t *testing.T) {
	tmpDir := t.TempDir()

	fileInfoMap := map[string]*fileInfo{
		"test.txt": {
			Index:      0,
			FullPath:   filepath.Join(tmpDir, "test.txt"),
			ChunkCount: 2,
			Record:     BuildManifestRecord{SizeInBytes: 2 * MaxChunkSize, SHA: "abc123"},
		},
	}

	fileChunks := map[int][]BuildManifestChunksRecord{
		0: {{ChunkSHA: "sha1"}, {ChunkSHA: "sha2"}},
	}

	checker := NewResumeChecker(tmpDir, fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	// File doesn't exist, should start from chunk 0
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

	// Create a complete file
	filePath := filepath.Join(tmpDir, "complete.txt")
	content := []byte("hello world")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Calculate SHA256
	hasher := sha256.New()
	hasher.Write(content)
	expectedSHA := hex.EncodeToString(hasher.Sum(nil))

	fileInfoMap := map[string]*fileInfo{
		"complete.txt": {
			Index:      0,
			FullPath:   filePath,
			ChunkCount: 1,
			Record:     BuildManifestRecord{SizeInBytes: len(content), SHA: expectedSHA},
		},
	}

	fileChunks := map[int][]BuildManifestChunksRecord{
		0: {{ChunkSHA: "sha1"}},
	}

	checker := NewResumeChecker(tmpDir, fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	// File is complete and verified
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

	// Create a file with wrong content (same size, different content)
	filePath := filepath.Join(tmpDir, "corrupted.txt")
	content := []byte("hello world")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	fileInfoMap := map[string]*fileInfo{
		"corrupted.txt": {
			Index:      0,
			FullPath:   filePath,
			ChunkCount: 1,
			Record:     BuildManifestRecord{SizeInBytes: len(content), SHA: "wronghash"},
		},
	}

	fileChunks := map[int][]BuildManifestChunksRecord{
		0: {{ChunkSHA: "sha1"}},
	}

	checker := NewResumeChecker(tmpDir, fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	// File is corrupted
	if state.CompletedFiles[0] {
		t.Error("file should not be marked complete")
	}
	if !state.CorruptedFiles[0] {
		t.Error("file should be marked corrupted")
	}
}

func TestResumeChecker_PartialFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a partial file with one complete chunk
	filePath := filepath.Join(tmpDir, "partial.txt")
	chunkData := make([]byte, MaxChunkSize)
	for i := range chunkData {
		chunkData[i] = byte(i % 256)
	}
	if err := os.WriteFile(filePath, chunkData, 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Calculate chunk SHA
	hasher := sha256.New()
	hasher.Write(chunkData)
	chunkSHA := hex.EncodeToString(hasher.Sum(nil))

	fileInfoMap := map[string]*fileInfo{
		"partial.txt": {
			Index:      0,
			FullPath:   filePath,
			ChunkCount: 3,
			Record:     BuildManifestRecord{SizeInBytes: 3 * MaxChunkSize, SHA: "fullfilehash"},
		},
	}

	fileChunks := map[int][]BuildManifestChunksRecord{
		0: {{ChunkSHA: chunkSHA}, {ChunkSHA: "chunk2"}, {ChunkSHA: "chunk3"}},
	}

	checker := NewResumeChecker(tmpDir, fileInfoMap, fileChunks, 4)
	state, err := checker.CheckExistingFiles()
	if err != nil {
		t.Fatalf("CheckExistingFiles failed: %v", err)
	}

	// File is partial, should resume from chunk 1
	if state.CompletedFiles[0] {
		t.Error("file should not be marked complete")
	}
	if state.CorruptedFiles[0] {
		t.Error("file should not be marked corrupted")
	}
	if state.StartChunkIndex[0] != 1 {
		t.Errorf("expected startChunk 1, got %d", state.StartChunkIndex[0])
	}
	if state.BytesAlreadyDownloaded != int64(MaxChunkSize) {
		t.Errorf("expected %d bytes already downloaded, got %d", MaxChunkSize, state.BytesAlreadyDownloaded)
	}
}

func TestFilterChunksToDownload(t *testing.T) {
	fileChunks := map[int][]BuildManifestChunksRecord{
		0: {{ChunkSHA: "a"}, {ChunkSHA: "b"}, {ChunkSHA: "c"}},
		1: {{ChunkSHA: "d"}, {ChunkSHA: "e"}},
		2: {{ChunkSHA: "f"}},
	}

	state := &ResumeState{
		CompletedFiles:  map[int]bool{1: true},   // File 1 is complete
		CorruptedFiles:  map[int]bool{2: true},   // File 2 is corrupted
		StartChunkIndex: map[int]int{0: 1, 2: 0}, // File 0 starts at chunk 1
	}

	filtered := FilterChunksToDownload(fileChunks, state)

	// File 0: should only have chunks b, c (starting from index 1)
	if len(filtered[0]) != 2 {
		t.Errorf("file 0: expected 2 chunks, got %d", len(filtered[0]))
	}
	if filtered[0][0].ChunkSHA != "b" {
		t.Errorf("file 0: expected first chunk 'b', got %q", filtered[0][0].ChunkSHA)
	}

	// File 1: should be skipped (complete)
	if _, ok := filtered[1]; ok {
		t.Error("file 1 should be skipped (complete)")
	}

	// File 2: should have all chunks (corrupted = re-download all)
	if len(filtered[2]) != 1 {
		t.Errorf("file 2: expected 1 chunk, got %d", len(filtered[2]))
	}
}

func TestHasExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// No files exist
	fileInfoMap := map[string]*fileInfo{
		"test.txt": {FullPath: filepath.Join(tmpDir, "test.txt")},
	}
	if hasExistingFiles(fileInfoMap) {
		t.Error("expected no existing files")
	}

	// Create a file
	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if !hasExistingFiles(fileInfoMap) {
		t.Error("expected existing files")
	}
}

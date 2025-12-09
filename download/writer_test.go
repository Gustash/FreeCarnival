package download

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/manifest"
)

func TestDiskWriter_EmptyChannel(t *testing.T) {
	memory := NewMemoryLimiter(1024 * 1024)
	writer := NewDiskWriter(memory, nil)

	chunks := make(chan ChunkResult)
	close(chunks)

	err := writer.WriteChunks(context.Background(), chunks, map[int]*FileInfo{}, map[int]int{}, nil)
	if err != nil {
		t.Errorf("WriteChunks failed for empty channel: %v", err)
	}
}

func TestDiskWriter_Success(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "writer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")

	memory := NewMemoryLimiter(manifest.MaxChunkSize * 3)
	writer := NewDiskWriter(memory, nil)

	info := &FileInfo{
		Index:      0,
		FullPath:   filePath,
		ChunkCount: 3,
	}

	fileInfoMap := map[int]*FileInfo{0: info}
	fileChunkCounts := map[int]int{0: 3}

	chunks := make(chan ChunkResult, 3)

	memory.Acquire(context.Background(), manifest.MaxChunkSize)
	memory.Acquire(context.Background(), manifest.MaxChunkSize)
	memory.Acquire(context.Background(), manifest.MaxChunkSize)

	chunks <- ChunkResult{FileIndex: 0, ChunkIndex: 2, Data: []byte("chunk2")}
	chunks <- ChunkResult{FileIndex: 0, ChunkIndex: 0, Data: []byte("chunk0")}
	chunks <- ChunkResult{FileIndex: 0, ChunkIndex: 1, Data: []byte("chunk1")}
	close(chunks)

	err = writer.WriteChunks(context.Background(), chunks, fileInfoMap, fileChunkCounts, nil)
	if err != nil {
		t.Fatalf("WriteChunks failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	expected := "chunk0chunk1chunk2"
	if string(data) != expected {
		t.Errorf("file contents = %q, expected %q", string(data), expected)
	}
}

func TestDiskWriter_ChunkError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "writer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")

	memory := NewMemoryLimiter(manifest.MaxChunkSize * 2)
	writer := NewDiskWriter(memory, nil)

	info := &FileInfo{
		Index:      0,
		FullPath:   filePath,
		ChunkCount: 2,
	}

	fileInfoMap := map[int]*FileInfo{0: info}
	fileChunkCounts := map[int]int{0: 2}

	chunks := make(chan ChunkResult, 2)

	chunks <- ChunkResult{FileIndex: 0, ChunkIndex: 0, Data: nil, Error: io.ErrUnexpectedEOF}
	close(chunks)

	err = writer.WriteChunks(context.Background(), chunks, fileInfoMap, fileChunkCounts, nil)
	if err == nil {
		t.Error("expected error when chunk has error")
	}
}

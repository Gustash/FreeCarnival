package update

import (
	"testing"

	"github.com/gustash/freecarnival/manifest"
)

func TestGenerateDelta(t *testing.T) {
	oldManifest := []manifest.BuildRecord{
		{FileName: "file1.txt", SHA: "abc123", SizeInBytes: 100},
		{FileName: "file2.txt", SHA: "def456", SizeInBytes: 200},
		{FileName: "file3.txt", SHA: "ghi789", SizeInBytes: 300},
	}

	newManifest := []manifest.BuildRecord{
		{FileName: "file1.txt", SHA: "abc123", SizeInBytes: 100}, // Unchanged
		{FileName: "file2.txt", SHA: "xyz999", SizeInBytes: 250}, // Modified (SHA changed)
		{FileName: "file4.txt", SHA: "new123", SizeInBytes: 400}, // Added
	}

	delta := GenerateDelta(oldManifest, newManifest)

	// Check added files
	if len(delta.Added) != 1 {
		t.Errorf("Expected 1 added file, got %d", len(delta.Added))
	}
	if len(delta.Added) > 0 && delta.Added[0].FileName != "file4.txt" {
		t.Errorf("Expected added file 'file4.txt', got '%s'", delta.Added[0].FileName)
	}

	// Check modified files
	if len(delta.Modified) != 1 {
		t.Errorf("Expected 1 modified file, got %d", len(delta.Modified))
	}
	if len(delta.Modified) > 0 && delta.Modified[0].FileName != "file2.txt" {
		t.Errorf("Expected modified file 'file2.txt', got '%s'", delta.Modified[0].FileName)
	}

	// Check removed files
	if len(delta.Removed) != 1 {
		t.Errorf("Expected 1 removed file, got %d", len(delta.Removed))
	}
	if len(delta.Removed) > 0 && delta.Removed[0].FileName != "file3.txt" {
		t.Errorf("Expected removed file 'file3.txt', got '%s'", delta.Removed[0].FileName)
	}
}

func TestGenerateDelta_NoChanges(t *testing.T) {
	manifest := []manifest.BuildRecord{
		{FileName: "file1.txt", SHA: "abc123", SizeInBytes: 100},
		{FileName: "file2.txt", SHA: "def456", SizeInBytes: 200},
	}

	delta := GenerateDelta(manifest, manifest)

	if !delta.IsEmpty() {
		t.Error("Expected empty delta for identical manifests")
	}
}

func TestFilterChunksForDelta(t *testing.T) {
	delta := &DeltaManifest{
		Added: []manifest.BuildRecord{
			{FileName: "new.txt", SizeInBytes: 100, Chunks: 1},
		},
		Modified: []manifest.BuildRecord{
			{FileName: "changed.txt", SizeInBytes: 200, Chunks: 2},
		},
		Removed: []manifest.BuildRecord{
			{FileName: "deleted.txt", SizeInBytes: 300, Chunks: 3},
		},
	}

	allChunks := []manifest.ChunkRecord{
		{FilePath: "new.txt", ID: 0, ChunkSHA: "chunk1"},
		{FilePath: "changed.txt", ID: 0, ChunkSHA: "chunk2"},
		{FilePath: "changed.txt", ID: 1, ChunkSHA: "chunk3"},
		{FilePath: "deleted.txt", ID: 0, ChunkSHA: "chunk4"}, // Should be filtered out
		{FilePath: "unchanged.txt", ID: 0, ChunkSHA: "chunk5"}, // Should be filtered out
	}

	filtered := FilterChunksForDelta(allChunks, delta)

	// Should only include chunks for added and modified files (3 total)
	if len(filtered) != 3 {
		t.Errorf("Expected 3 filtered chunks, got %d", len(filtered))
	}

	// Verify deleted.txt chunks were excluded
	for _, chunk := range filtered {
		if chunk.FilePath == "deleted.txt" || chunk.FilePath == "unchanged.txt" {
			t.Errorf("Unexpected chunk for file %s", chunk.FilePath)
		}
	}
}


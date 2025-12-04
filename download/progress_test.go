package download

import (
	"testing"
	"time"
)

func TestNewProgressTracker(t *testing.T) {
	pt := NewProgressTracker(1000, 5)
	defer pt.Wait()

	if pt.totalBytes != 1000 {
		t.Errorf("totalBytes = %d, expected 1000", pt.totalBytes)
	}
	if pt.totalFiles != 5 {
		t.Errorf("totalFiles = %d, expected 5", pt.totalFiles)
	}
}

func TestProgressTracker_AddFile(t *testing.T) {
	pt := NewProgressTracker(1000, 2)
	defer pt.Wait()

	pt.AddFile(0, "file1.txt", 5, 500)
	pt.AddFile(1, "file2.txt", 3, 500)

	pt.filesMu.RLock()
	defer pt.filesMu.RUnlock()

	if len(pt.files) != 2 {
		t.Errorf("expected 2 files, got %d", len(pt.files))
	}

	fp1 := pt.files[0]
	if fp1.fileName != "file1.txt" {
		t.Errorf("file[0].fileName = %q, expected %q", fp1.fileName, "file1.txt")
	}
	if fp1.totalChunks != 5 {
		t.Errorf("file[0].totalChunks = %d, expected 5", fp1.totalChunks)
	}
	if fp1.totalSize != 500 {
		t.Errorf("file[0].totalSize = %d, expected 500", fp1.totalSize)
	}
}

func TestProgressTracker_ChunkDownloaded(t *testing.T) {
	pt := NewProgressTracker(1000, 1)
	defer pt.Wait()

	pt.ChunkDownloaded(0, 100)
	pt.ChunkDownloaded(0, 200)

	downloaded := pt.downloadedBytes.Load()
	if downloaded != 300 {
		t.Errorf("downloadedBytes = %d, expected 300", downloaded)
	}
}

func TestProgressTracker_ChunkWritten(t *testing.T) {
	pt := NewProgressTracker(1000, 1)
	defer pt.Wait()

	pt.AddFile(0, "file.txt", 3, 300)

	pt.ChunkWritten(0, 100)
	pt.ChunkWritten(0, 100)

	written := pt.writtenBytes.Load()
	if written != 200 {
		t.Errorf("writtenBytes = %d, expected 200", written)
	}

	pt.filesMu.RLock()
	fp := pt.files[0]
	pt.filesMu.RUnlock()

	if fp.chunksWritten.Load() != 2 {
		t.Errorf("file.chunksWritten = %d, expected 2", fp.chunksWritten.Load())
	}
}

func TestProgressTracker_FileComplete(t *testing.T) {
	pt := NewProgressTracker(1000, 2)
	defer pt.Wait()

	pt.AddFile(0, "file1.txt", 5, 500)
	pt.AddFile(1, "file2.txt", 3, 500)

	pt.FileComplete(0)

	if pt.completedFiles.Load() != 1 {
		t.Errorf("completedFiles = %d, expected 1", pt.completedFiles.Load())
	}

	pt.filesMu.RLock()
	fp := pt.files[0]
	pt.filesMu.RUnlock()

	if !fp.complete {
		t.Error("file[0] should be marked complete")
	}
	if fp.chunksWritten.Load() != 5 {
		t.Errorf("file[0].chunksWritten should be set to totalChunks (5), got %d", fp.chunksWritten.Load())
	}
}

func TestProgressTracker_GetStats(t *testing.T) {
	pt := NewProgressTracker(1000, 5)
	defer pt.Wait()

	pt.FileComplete(0)
	pt.FileComplete(1)

	// Allow time for speed calculation
	time.Sleep(150 * time.Millisecond)

	_, _, completed, total := pt.GetStats()

	if completed != 2 {
		t.Errorf("completed = %d, expected 2", completed)
	}
	if total != 5 {
		t.Errorf("total = %d, expected 5", total)
	}
}

func TestProgressTracker_Wait(t *testing.T) {
	pt := NewProgressTracker(100, 1)

	// Wait should complete without blocking indefinitely
	done := make(chan struct{})
	go func() {
		pt.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Wait() did not return in time")
	}
}

func TestProgressTracker_Abort(t *testing.T) {
	pt := NewProgressTracker(100, 1)

	// Abort should complete without blocking
	done := make(chan struct{})
	go func() {
		pt.Abort()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("Abort() did not return in time")
	}
}

func TestProgressTracker_ConcurrentAccess(t *testing.T) {
	pt := NewProgressTracker(10000, 10)

	// Add files
	for i := 0; i < 10; i++ {
		pt.AddFile(i, "file.txt", 10, 1000)
	}

	// Concurrent chunk downloads and writes
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			pt.ChunkDownloaded(i%10, 100)
		}
		close(done)
	}()

	go func() {
		for i := 0; i < 100; i++ {
			pt.ChunkWritten(i%10, 100)
		}
	}()

	go func() {
		for i := 0; i < 10; i++ {
			pt.FileComplete(i)
		}
	}()

	<-done
	pt.Wait()

	// Should complete without race conditions
	if pt.completedFiles.Load() != 10 {
		t.Errorf("completedFiles = %d, expected 10", pt.completedFiles.Load())
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1572864, "1.50 MB"},
		{1073741824, "1.00 GB"},
		{1610612736, "1.50 GB"},
		{1099511627776, "1.00 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatBytes(%d) = %q, expected %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

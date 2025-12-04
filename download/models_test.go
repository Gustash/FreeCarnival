package download

import (
	"testing"
)

func TestBuildManifestRecord_IsDirectory(t *testing.T) {
	tests := []struct {
		name     string
		flags    int
		expected bool
	}{
		{"directory flag 40", 40, true},
		{"file flag 0", 0, false},
		{"other flag 1", 1, false},
		{"other flag 32", 32, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &BuildManifestRecord{Flags: tt.flags}
			if got := r.IsDirectory(); got != tt.expected {
				t.Errorf("IsDirectory() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestBuildManifestRecord_IsEmpty(t *testing.T) {
	tests := []struct {
		name        string
		sizeInBytes int
		expected    bool
	}{
		{"empty file", 0, true},
		{"non-empty file", 100, false},
		{"large file", 1048576, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &BuildManifestRecord{SizeInBytes: tt.sizeInBytes}
			if got := r.IsEmpty(); got != tt.expected {
				t.Errorf("IsEmpty() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultDownloadOptions(t *testing.T) {
	opts := DefaultDownloadOptions()

	if opts.MaxDownloadWorkers != DefaultMaxDownloadWorkers {
		t.Errorf("MaxDownloadWorkers = %d, expected %d", opts.MaxDownloadWorkers, DefaultMaxDownloadWorkers)
	}

	if opts.MaxMemoryUsage != DefaultMaxMemoryUsage {
		t.Errorf("MaxMemoryUsage = %d, expected %d", opts.MaxMemoryUsage, DefaultMaxMemoryUsage)
	}

	if opts.SkipVerify {
		t.Error("SkipVerify should be false by default")
	}

	if opts.InfoOnly {
		t.Error("InfoOnly should be false by default")
	}
}

func TestDefaultMaxDownloadWorkers(t *testing.T) {
	// Workers should be at least 2 and at most 16
	if DefaultMaxDownloadWorkers < 2 {
		t.Errorf("DefaultMaxDownloadWorkers = %d, expected at least 2", DefaultMaxDownloadWorkers)
	}
	if DefaultMaxDownloadWorkers > 16 {
		t.Errorf("DefaultMaxDownloadWorkers = %d, expected at most 16", DefaultMaxDownloadWorkers)
	}
}

func TestDefaultMaxMemoryUsage(t *testing.T) {
	// Should be 1 GiB (1024 * MaxChunkSize)
	expectedMemory := MaxChunkSize * 1024
	if DefaultMaxMemoryUsage != expectedMemory {
		t.Errorf("DefaultMaxMemoryUsage = %d, expected %d", DefaultMaxMemoryUsage, expectedMemory)
	}
}

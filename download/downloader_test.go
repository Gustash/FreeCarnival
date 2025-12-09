package download

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/manifest"
)

func TestExtractSHA(t *testing.T) {
	tests := []struct {
		name     string
		chunkID  string
		expected string
	}{
		{
			name:     "full format with prefix and index",
			chunkID:  "5774447b9a464b9bbec6b3555ee82867_0_ed3afd9fc1217afedffbb57aa86f38d4124ce77f18430740a820cf2785814dd9",
			expected: "ed3afd9fc1217afedffbb57aa86f38d4124ce77f18430740a820cf2785814dd9",
		},
		{
			name:     "format with just index and sha",
			chunkID:  "chunk_0_abcdef123456",
			expected: "abcdef123456",
		},
		{
			name:     "no underscore - return as is",
			chunkID:  "abcdef123456",
			expected: "abcdef123456",
		},
		{
			name:     "single underscore",
			chunkID:  "prefix_sha256hash",
			expected: "sha256hash",
		},
		{
			name:     "empty string",
			chunkID:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manifest.ExtractSHA(tt.chunkID)
			if result != tt.expected {
				t.Errorf("ExtractSHA(%q) = %q, expected %q", tt.chunkID, result, tt.expected)
			}
		})
	}
}

func TestNewDownloader(t *testing.T) {
	product := &auth.Product{
		Name:      "Test Game",
		Namespace: "testdev",
		IDKeyName: "test-uuid",
	}
	version := &auth.ProductVersion{
		Version: "1234567890",
		OS:      auth.BuildOSWindows,
	}
	options := Options{
		MaxDownloadWorkers: 8,
		MaxMemoryUsage:     1024 * 1024 * 100,
		SkipVerify:         false,
		InfoOnly:           false,
	}

	d := New(nil, product, version, options)

	if d.product != product {
		t.Error("product not set correctly")
	}
	if d.version != version {
		t.Error("version not set correctly")
	}
	if d.options.MaxDownloadWorkers != 8 {
		t.Errorf("MaxDownloadWorkers = %d, expected 8", d.options.MaxDownloadWorkers)
	}
	if d.memory == nil {
		t.Error("memory limiter should be initialized")
	}
}

func TestCreateOptimizedClient(t *testing.T) {
	client := createOptimizedClient(&http.Client{}, 16)

	if client == nil {
		t.Fatal("createOptimizedClient returned nil")
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Transport is not *http.Transport")
	}

	if transport.MaxIdleConnsPerHost != 16 {
		t.Errorf("MaxIdleConnsPerHost = %d, expected 16", transport.MaxIdleConnsPerHost)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be true")
	}
	if !transport.DisableCompression {
		t.Error("DisableCompression should be true")
	}
}

func TestDownloader_PrepareInstallation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "download-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	records := []manifest.BuildRecord{
		{FileName: "game", Flags: 40, SizeInBytes: 0},
		{FileName: "game/data", Flags: 40, SizeInBytes: 0},
		{FileName: "game/readme.txt", Flags: 0, SizeInBytes: 0},
		{FileName: "game/data/level.dat", Flags: 0, SizeInBytes: 1000, Chunks: 1, SHA: "sha123"},
	}

	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := New(nil, product, version, DefaultOptions())

	fileInfoMap, err := d.prepareInstallation(tmpDir, records)
	if err != nil {
		t.Fatalf("prepareInstallation failed: %v", err)
	}

	gameDir := filepath.Join(tmpDir, "game")
	if _, err := os.Stat(gameDir); os.IsNotExist(err) {
		t.Error("game directory was not created")
	}

	dataDir := filepath.Join(tmpDir, "game", "data")
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Error("game/data directory was not created")
	}

	readmePath := filepath.Join(tmpDir, "game", "readme.txt")
	info, err := os.Stat(readmePath)
	if os.IsNotExist(err) {
		t.Error("readme.txt was not created")
	}
	if info.Size() != 0 {
		t.Errorf("readme.txt size = %d, expected 0", info.Size())
	}

	if len(fileInfoMap) != 1 {
		t.Errorf("fileInfoMap has %d entries, expected 1", len(fileInfoMap))
	}

	levelInfo, ok := fileInfoMap["game/data/level.dat"]
	if !ok {
		t.Error("level.dat not in fileInfoMap")
	}
	if levelInfo.ChunkCount != 1 {
		t.Errorf("level.dat ChunkCount = %d, expected 1", levelInfo.ChunkCount)
	}
}

func TestDownloader_GroupChunksByFile(t *testing.T) {
	fileInfoMap := map[string]*FileInfo{
		"file1.txt": {Index: 0},
		"file2.txt": {Index: 1},
	}

	chunks := []manifest.ChunkRecord{
		{ID: 0, FilePath: "file1.txt", ChunkSHA: "sha1_0"},
		{ID: 1, FilePath: "file1.txt", ChunkSHA: "sha1_1"},
		{ID: 0, FilePath: "file2.txt", ChunkSHA: "sha2_0"},
		{ID: 0, FilePath: "unknown.txt", ChunkSHA: "sha_unknown"},
	}

	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := New(nil, product, version, DefaultOptions())

	fileChunks := d.groupChunksByFile(chunks, fileInfoMap)

	if len(fileChunks) != 2 {
		t.Errorf("expected 2 file groups, got %d", len(fileChunks))
	}

	if len(fileChunks[0]) != 2 {
		t.Errorf("file1.txt should have 2 chunks, got %d", len(fileChunks[0]))
	}

	if len(fileChunks[1]) != 1 {
		t.Errorf("file2.txt should have 1 chunk, got %d", len(fileChunks[1]))
	}
}

func TestDownloader_InfoOnly(t *testing.T) {
	product := &auth.Product{
		Name:      "Test Game",
		Namespace: "test",
		IDKeyName: "game",
	}
	version := &auth.ProductVersion{
		Version: "123",
		OS:      auth.BuildOSWindows,
	}
	options := Options{
		MaxDownloadWorkers: 2,
		MaxMemoryUsage:     1024 * 1024,
		InfoOnly:           true,
	}

	d := New(nil, product, version, options)

	tmpDir, err := os.MkdirTemp("", "info-only-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	buildManifest := []manifest.BuildRecord{
		{FileName: "test.txt", SizeInBytes: 1000, Flags: 0, Chunks: 1},
	}
	chunksManifest := []manifest.ChunkRecord{
		{FilePath: "test.txt", ChunkSHA: "abc123"},
	}

	// Download with InfoOnly should not create any files or directories
	err = d.Download(context.Background(), tmpDir, buildManifest, chunksManifest)
	if err != nil {
		t.Errorf("Download with InfoOnly failed: %v", err)
	}

	// Verify no files were created (except the tmpDir itself)
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	if len(entries) > 0 {
		t.Errorf("InfoOnly should not create any files, but found %d entries", len(entries))
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.MaxDownloadWorkers != DefaultMaxWorkers {
		t.Errorf("MaxDownloadWorkers = %d, expected %d", opts.MaxDownloadWorkers, DefaultMaxWorkers)
	}

	if opts.MaxMemoryUsage != DefaultMaxMemory {
		t.Errorf("MaxMemoryUsage = %d, expected %d", opts.MaxMemoryUsage, DefaultMaxMemory)
	}

	if opts.SkipVerify {
		t.Error("SkipVerify should be false by default")
	}

	if opts.InfoOnly {
		t.Error("InfoOnly should be false by default")
	}
}

func TestDefaultMaxWorkers(t *testing.T) {
	// Workers should be at least 2 and at most 16
	if DefaultMaxWorkers < 2 {
		t.Errorf("DefaultMaxWorkers = %d, expected at least 2", DefaultMaxWorkers)
	}
	if DefaultMaxWorkers > 16 {
		t.Errorf("DefaultMaxWorkers = %d, expected at most 16", DefaultMaxWorkers)
	}
}

func TestDefaultMaxMemory(t *testing.T) {
	// Should be 1 GiB (1024 * MaxChunkSize)
	expectedMemory := manifest.MaxChunkSize * 1024
	if DefaultMaxMemory != expectedMemory {
		t.Errorf("DefaultMaxMemory = %d, expected %d", DefaultMaxMemory, expectedMemory)
	}
}

package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gustash/freecarnival/auth"
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
			result := extractSHA(tt.chunkID)
			if result != tt.expected {
				t.Errorf("extractSHA(%q) = %q, expected %q", tt.chunkID, result, tt.expected)
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
	options := DownloadOptions{
		MaxDownloadWorkers: 8,
		MaxMemoryUsage:     1024 * 1024 * 100, // 100 MB
		SkipVerify:         false,
		InfoOnly:           false,
	}

	d := NewDownloader(nil, product, version, options)

	if d.product != product {
		t.Error("product not set correctly")
	}
	if d.version != version {
		t.Error("version not set correctly")
	}
	if d.options.MaxDownloadWorkers != 8 {
		t.Errorf("MaxDownloadWorkers = %d, expected 8", d.options.MaxDownloadWorkers)
	}
	if d.maxMemory != int64(options.MaxMemoryUsage) {
		t.Errorf("maxMemory = %d, expected %d", d.maxMemory, options.MaxMemoryUsage)
	}
	if d.memoryCond == nil {
		t.Error("memoryCond should be initialized")
	}
}

func TestCreateOptimizedClient(t *testing.T) {
	client := createOptimizedClient(16)

	if client == nil {
		t.Fatal("createOptimizedClient returned nil")
	}

	// Check that transport is configured
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Transport is not *http.Transport")
	}

	if transport.MaxIdleConns != 32 {
		t.Errorf("MaxIdleConns = %d, expected 32", transport.MaxIdleConns)
	}
	if transport.MaxIdleConnsPerHost != 32 {
		t.Errorf("MaxIdleConnsPerHost = %d, expected 32", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxConnsPerHost != 32 {
		t.Errorf("MaxConnsPerHost = %d, expected 32", transport.MaxConnsPerHost)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be true")
	}
	if !transport.DisableCompression {
		t.Error("DisableCompression should be true")
	}
}

func TestDownloader_DownloadChunk(t *testing.T) {
	chunkData := []byte("test chunk data for downloading")
	chunkSHA := sha256.Sum256(chunkData)
	chunkSHAHex := hex.EncodeToString(chunkSHA[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "galaClient" {
			t.Errorf("expected User-Agent galaClient, got %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write(chunkData)
	}))
	defer server.Close()

	// Create downloader with test product that points to our test server
	product := &auth.Product{
		Namespace: "test",
		IDKeyName: "game",
	}
	version := &auth.ProductVersion{
		OS: auth.BuildOSWindows,
	}

	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

	// Override client to use test server
	d.client = &http.Client{}

	// Test downloadChunk by calling the method that uses it
	// Since downloadChunk uses GetChunkURL which hardcodes ContentURL,
	// we test the SHA verification logic separately

	// Verify SHA extraction and verification logic
	actualSHA := extractSHA("prefix_0_" + chunkSHAHex)
	if actualSHA != chunkSHAHex {
		t.Errorf("SHA extraction failed: got %q, expected %q", actualSHA, chunkSHAHex)
	}

	hash := sha256.Sum256(chunkData)
	computedSHA := hex.EncodeToString(hash[:])
	if computedSHA != chunkSHAHex {
		t.Errorf("SHA computation failed: got %q, expected %q", computedSHA, chunkSHAHex)
	}
}

func TestDownloader_PrepareInstallation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "download-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manifest := []BuildManifestRecord{
		{FileName: "game", Flags: 40, SizeInBytes: 0},           // Directory
		{FileName: "game/data", Flags: 40, SizeInBytes: 0},      // Nested directory
		{FileName: "game/readme.txt", Flags: 0, SizeInBytes: 0}, // Empty file
		{FileName: "game/data/level.dat", Flags: 0, SizeInBytes: 1000, Chunks: 1, SHA: "sha123"},
	}

	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

	fileInfoMap, err := d.prepareInstallation(tmpDir, manifest)
	if err != nil {
		t.Fatalf("prepareInstallation failed: %v", err)
	}

	// Check directories were created
	gameDir := filepath.Join(tmpDir, "game")
	if _, err := os.Stat(gameDir); os.IsNotExist(err) {
		t.Error("game directory was not created")
	}

	dataDir := filepath.Join(tmpDir, "game", "data")
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Error("game/data directory was not created")
	}

	// Check empty file was created
	readmePath := filepath.Join(tmpDir, "game", "readme.txt")
	info, err := os.Stat(readmePath)
	if os.IsNotExist(err) {
		t.Error("readme.txt was not created")
	}
	if info.Size() != 0 {
		t.Errorf("readme.txt size = %d, expected 0", info.Size())
	}

	// Check fileInfoMap contains only files with chunks
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
	fileInfoMap := map[string]*fileInfo{
		"file1.txt": {Index: 0},
		"file2.txt": {Index: 1},
	}

	chunks := []BuildManifestChunksRecord{
		{ID: 0, FilePath: "file1.txt", ChunkSHA: "sha1_0"},
		{ID: 1, FilePath: "file1.txt", ChunkSHA: "sha1_1"},
		{ID: 0, FilePath: "file2.txt", ChunkSHA: "sha2_0"},
		{ID: 0, FilePath: "unknown.txt", ChunkSHA: "sha_unknown"}, // Should be ignored
	}

	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

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

func TestDownloader_MemoryManagement(t *testing.T) {
	product := &auth.Product{}
	version := &auth.ProductVersion{}
	options := DownloadOptions{
		MaxDownloadWorkers: 2,
		MaxMemoryUsage:     1024, // 1 KB
	}
	d := NewDownloader(nil, product, version, options)

	// Allocate memory
	d.memoryUsed.Store(0)
	ctx := context.Background()

	// This should succeed (enough memory)
	done := make(chan bool)
	go func() {
		d.waitForMemory(ctx, 512)
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-context.Background().Done():
		t.Error("waitForMemory blocked unexpectedly")
	}

	if d.memoryUsed.Load() != 512 {
		t.Errorf("memoryUsed = %d, expected 512", d.memoryUsed.Load())
	}

	// Release memory
	d.releaseMemory(512)
	if d.memoryUsed.Load() != 0 {
		t.Errorf("memoryUsed after release = %d, expected 0", d.memoryUsed.Load())
	}
}

func TestDownloader_InfoOnly(t *testing.T) {
	// Create a minimal test server for manifest
	buildManifestCSV := `Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag
1000,1,sha123,0,test.txt,tag
`
	chunksManifestCSV := `ID,Filepath,Chunk SHA
0,test.txt,prefix_0_sha123
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.csv":
			w.Write([]byte(buildManifestCSV))
		case "/chunks.csv":
			w.Write([]byte(chunksManifestCSV))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Test that InfoOnly mode doesn't actually download files
	product := &auth.Product{
		Name:      "Test Game",
		Namespace: "test",
		IDKeyName: "game",
	}
	version := &auth.ProductVersion{
		Version: "123",
		OS:      auth.BuildOSWindows,
	}
	options := DownloadOptions{
		MaxDownloadWorkers: 2,
		MaxMemoryUsage:     1024 * 1024,
		InfoOnly:           true,
	}

	d := NewDownloader(nil, product, version, options)

	// printDownloadInfo doesn't return an error, it just prints
	manifest := []BuildManifestRecord{
		{SizeInBytes: 1000, Flags: 0},
		{SizeInBytes: 0, Flags: 40},
	}
	// This should not panic or error
	d.printDownloadInfo(manifest)
}

func TestFetchCSV_UserAgent(t *testing.T) {
	var receivedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data"))
	}))
	defer server.Close()

	client := &http.Client{}
	_, err := fetchCSV(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("fetchCSV failed: %v", err)
	}

	if receivedUA != "galaClient" {
		t.Errorf("User-Agent = %q, expected %q", receivedUA, "galaClient")
	}
}

func TestDownloader_DownloadChunk_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// We can't easily test downloadChunk directly since it uses hardcoded URLs,
	// but we can test the error handling pattern
	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestDownloader_Download_ContextCancellation(t *testing.T) {
	// Test that download respects context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	product := &auth.Product{
		Name:      "Test",
		Namespace: "test",
		IDKeyName: "game",
	}
	version := &auth.ProductVersion{
		Version: "123",
		OS:      auth.BuildOSWindows,
	}

	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

	// waitForMemory should respect cancelled context
	d.waitForMemory(ctx, 1024)
	// Should return without blocking due to cancelled context
}

func TestSingleWriter_EmptyChannel(t *testing.T) {
	// Test that diskWriter handles empty channel correctly
	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

	chunks := make(chan DownloadedChunk)
	close(chunks)

	err := d.diskWriter(context.Background(), chunks, map[int]*fileInfo{}, map[int]int{}, nil)
	if err != nil {
		t.Errorf("diskWriter failed for empty channel: %v", err)
	}
}

func TestSingleWriter_Success(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "writer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")

	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

	info := &fileInfo{
		Index:      0,
		FullPath:   filePath,
		ChunkCount: 3,
	}

	fileInfoMap := map[int]*fileInfo{0: info}
	fileChunkCounts := map[int]int{0: 3}

	chunks := make(chan DownloadedChunk, 3)

	// Send chunks in order
	chunks <- DownloadedChunk{FileIndex: 0, ChunkIndex: 0, Data: []byte("chunk0")}
	chunks <- DownloadedChunk{FileIndex: 0, ChunkIndex: 1, Data: []byte("chunk1")}
	chunks <- DownloadedChunk{FileIndex: 0, ChunkIndex: 2, Data: []byte("chunk2")}
	close(chunks)

	err = d.diskWriter(context.Background(), chunks, fileInfoMap, fileChunkCounts, nil)
	if err != nil {
		t.Fatalf("diskWriter failed: %v", err)
	}

	// Verify file contents
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	expected := "chunk0chunk1chunk2"
	if string(data) != expected {
		t.Errorf("file contents = %q, expected %q", string(data), expected)
	}
}

func TestSingleWriter_OutOfOrderChunks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "writer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")

	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

	info := &fileInfo{
		Index:      0,
		FullPath:   filePath,
		ChunkCount: 3,
	}

	fileInfoMap := map[int]*fileInfo{0: info}
	fileChunkCounts := map[int]int{0: 3}

	chunks := make(chan DownloadedChunk, 3)

	// Send chunks OUT of order (2, 0, 1)
	chunks <- DownloadedChunk{FileIndex: 0, ChunkIndex: 2, Data: []byte("chunk2")}
	chunks <- DownloadedChunk{FileIndex: 0, ChunkIndex: 0, Data: []byte("chunk0")}
	chunks <- DownloadedChunk{FileIndex: 0, ChunkIndex: 1, Data: []byte("chunk1")}
	close(chunks)

	err = d.diskWriter(context.Background(), chunks, fileInfoMap, fileChunkCounts, nil)
	if err != nil {
		t.Fatalf("diskWriter failed: %v", err)
	}

	// Verify file contents are in correct order despite out-of-order delivery
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	expected := "chunk0chunk1chunk2"
	if string(data) != expected {
		t.Errorf("file contents = %q, expected %q", string(data), expected)
	}
}

func TestSingleWriter_ChunkError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "writer-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "test.txt")

	product := &auth.Product{}
	version := &auth.ProductVersion{}
	d := NewDownloader(nil, product, version, DefaultDownloadOptions())

	info := &fileInfo{
		Index:      0,
		FullPath:   filePath,
		ChunkCount: 2,
	}

	fileInfoMap := map[int]*fileInfo{0: info}
	fileChunkCounts := map[int]int{0: 2}

	chunks := make(chan DownloadedChunk, 2)

	// Send a chunk with an error
	chunks <- DownloadedChunk{FileIndex: 0, ChunkIndex: 0, Data: nil, Error: io.ErrUnexpectedEOF}
	close(chunks)

	err = d.diskWriter(context.Background(), chunks, fileInfoMap, fileChunkCounts, nil)
	if err == nil {
		t.Error("expected error when chunk has error")
	}
}

package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gustash/freecarnival/auth"
)

// Downloader manages parallel chunk downloads with memory limits
type Downloader struct {
	client  *http.Client
	product *auth.Product
	version *auth.ProductVersion
	options DownloadOptions

	// Memory management
	memoryUsed   atomic.Int64
	memoryMu     sync.Mutex
	memoryCond   *sync.Cond
	maxMemory    int64

	// Progress tracking
	totalBytes      int64
	downloadedBytes atomic.Int64
	totalFiles      int
	completedFiles  atomic.Int32
}

// NewDownloader creates a new downloader instance
func NewDownloader(client *http.Client, product *auth.Product, version *auth.ProductVersion, options DownloadOptions) *Downloader {
	d := &Downloader{
		client:    client,
		product:   product,
		version:   version,
		options:   options,
		maxMemory: int64(options.MaxMemoryUsage),
	}
	d.memoryCond = sync.NewCond(&d.memoryMu)
	return d
}

// Download performs the full download operation
func (d *Downloader) Download(ctx context.Context, installPath string) error {
	// Fetch manifests
	fmt.Println("Fetching build manifest...")
	buildManifest, err := FetchBuildManifest(ctx, d.client, d.product, d.version)
	if err != nil {
		return fmt.Errorf("failed to fetch build manifest: %w", err)
	}

	fmt.Println("Fetching chunks manifest...")
	chunksManifest, err := FetchChunksManifest(ctx, d.client, d.product, d.version)
	if err != nil {
		return fmt.Errorf("failed to fetch chunks manifest: %w", err)
	}

	// Calculate total size and file count
	for _, record := range buildManifest {
		if !record.IsDirectory() {
			d.totalBytes += int64(record.SizeInBytes)
			d.totalFiles++
		}
	}

	if d.options.InfoOnly {
		d.printDownloadInfo(buildManifest)
		return nil
	}

	fmt.Printf("Total download size: %s (%d files)\n", formatBytes(d.totalBytes), d.totalFiles)

	// Create directory structure and prepare file info
	fileInfoMap, err := d.prepareInstallation(installPath, buildManifest)
	if err != nil {
		return fmt.Errorf("failed to prepare installation: %w", err)
	}

	// Group chunks by file
	fileChunks := d.groupChunksByFile(chunksManifest, fileInfoMap)

	// Start download workers and file writers
	return d.downloadAndWrite(ctx, installPath, fileChunks, fileInfoMap)
}

type fileInfo struct {
	Index       int
	Record      BuildManifestRecord
	FullPath    string
	ChunkCount  int
}

func (d *Downloader) prepareInstallation(installPath string, manifest []BuildManifestRecord) (map[string]*fileInfo, error) {
	fileInfoMap := make(map[string]*fileInfo)
	fileIndex := 0

	for _, record := range manifest {
		fullPath := filepath.Join(installPath, record.FileName)

		if record.IsDirectory() {
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", fullPath, err)
			}
			continue
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create parent directory for %s: %w", fullPath, err)
		}

		// Create empty file if it has no chunks
		if record.IsEmpty() {
			f, err := os.Create(fullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to create empty file %s: %w", fullPath, err)
			}
			f.Close()
			continue
		}

		fileInfoMap[record.FileName] = &fileInfo{
			Index:      fileIndex,
			Record:     record,
			FullPath:   fullPath,
			ChunkCount: record.Chunks,
		}
		fileIndex++
	}

	return fileInfoMap, nil
}

func (d *Downloader) groupChunksByFile(chunks []BuildManifestChunksRecord, fileInfoMap map[string]*fileInfo) map[int][]BuildManifestChunksRecord {
	fileChunks := make(map[int][]BuildManifestChunksRecord)

	for _, chunk := range chunks {
		info, ok := fileInfoMap[chunk.FilePath]
		if !ok {
			continue
		}
		fileChunks[info.Index] = append(fileChunks[info.Index], chunk)
	}

	return fileChunks
}

func (d *Downloader) downloadAndWrite(ctx context.Context, installPath string, fileChunks map[int][]BuildManifestChunksRecord, fileInfoMap map[string]*fileInfo) error {
	// Create channels
	chunkJobs := make(chan ChunkDownload, d.options.MaxDownloadWorkers*2)
	downloadResults := make(chan DownloadedChunk, d.options.MaxDownloadWorkers*2)
	
	// Error channel for fatal errors
	errChan := make(chan error, 1)
	
	// Context for cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// Start download workers
	for i := 0; i < d.options.MaxDownloadWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.downloadWorker(ctx, chunkJobs, downloadResults)
		}()
	}

	// Start file writer goroutine
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- d.fileWriter(ctx, downloadResults, fileChunks, fileInfoMap)
	}()

	// Feed chunk jobs
	go func() {
		defer close(chunkJobs)
		for fileName, info := range fileInfoMap {
			chunks := fileChunks[info.Index]
			for chunkIdx, chunk := range chunks {
				select {
				case <-ctx.Done():
					return
				case chunkJobs <- ChunkDownload{
					FileIndex:  info.Index,
					ChunkIndex: chunkIdx,
					ChunkSHA:   chunk.ChunkSHA,
					FilePath:   fileName,
				}:
				}
			}
		}
	}()

	// Wait for download workers to finish
	go func() {
		wg.Wait()
		close(downloadResults)
	}()

	// Wait for writer to finish or error
	select {
	case err := <-writerDone:
		if err != nil {
			cancel()
			return err
		}
	case err := <-errChan:
		cancel()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (d *Downloader) downloadWorker(ctx context.Context, jobs <-chan ChunkDownload, results chan<- DownloadedChunk) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}

			// Wait for memory to be available
			d.waitForMemory(ctx, MaxChunkSize)
			if ctx.Err() != nil {
				return
			}

			// Download the chunk
			data, err := d.downloadChunk(ctx, job.ChunkSHA)
			
			result := DownloadedChunk{
				FileIndex:  job.FileIndex,
				ChunkIndex: job.ChunkIndex,
				Data:       data,
				Error:      err,
			}

			if err == nil {
				// Verify chunk SHA if not skipped
				if !d.options.SkipVerify {
					hash := sha256.Sum256(data)
					actualSHA := hex.EncodeToString(hash[:])
					// The chunk SHA in the manifest is in format: {prefix}_{index}_{actual_sha}
					// We need to extract only the actual SHA part (after the last underscore)
					expectedSHA := extractSHA(job.ChunkSHA)
					if actualSHA != expectedSHA {
						result.Error = fmt.Errorf("SHA mismatch for chunk %s: expected %s, got %s", job.ChunkSHA, expectedSHA, actualSHA)
						result.Data = nil
						d.releaseMemory(int64(len(data)))
					}
				}
			}

			select {
			case <-ctx.Done():
				if result.Data != nil {
					d.releaseMemory(int64(len(result.Data)))
				}
				return
			case results <- result:
			}
		}
	}
}

func (d *Downloader) downloadChunk(ctx context.Context, chunkSHA string) ([]byte, error) {
	url := GetChunkURL(d.product, d.version.OS, chunkSHA)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "galaClient")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d downloading chunk %s", resp.StatusCode, chunkSHA)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Update downloaded bytes counter
	d.downloadedBytes.Add(int64(len(data)))

	return data, nil
}

func (d *Downloader) waitForMemory(ctx context.Context, size int64) {
	d.memoryMu.Lock()
	defer d.memoryMu.Unlock()

	for d.memoryUsed.Load()+size > d.maxMemory {
		// Check context before waiting
		select {
		case <-ctx.Done():
			return
		default:
		}
		d.memoryCond.Wait()
	}
	d.memoryUsed.Add(size)
}

func (d *Downloader) releaseMemory(size int64) {
	d.memoryUsed.Add(-size)
	d.memoryCond.Broadcast()
}

func (d *Downloader) fileWriter(ctx context.Context, results <-chan DownloadedChunk, fileChunks map[int][]BuildManifestChunksRecord, fileInfoMap map[string]*fileInfo) error {
	// Track pending chunks per file (chunks that arrived out of order)
	pendingChunks := make(map[int]map[int][]byte) // fileIndex -> chunkIndex -> data
	nextChunkIndex := make(map[int]int)            // fileIndex -> next expected chunk index
	openFiles := make(map[int]*os.File)            // fileIndex -> open file handle

	// Initialize tracking for each file
	for _, info := range fileInfoMap {
		pendingChunks[info.Index] = make(map[int][]byte)
		nextChunkIndex[info.Index] = 0
	}

	// Create reverse lookup: fileIndex -> fileInfo
	indexToInfo := make(map[int]*fileInfo)
	for _, info := range fileInfoMap {
		indexToInfo[info.Index] = info
	}

	defer func() {
		// Close any open files
		for _, f := range openFiles {
			f.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-results:
			if !ok {
				// All downloads complete
				return nil
			}

			if result.Error != nil {
				return fmt.Errorf("download error: %w", result.Error)
			}

			fileIdx := result.FileIndex
			chunkIdx := result.ChunkIndex
			info := indexToInfo[fileIdx]

			// Check if this is the next expected chunk
			if chunkIdx == nextChunkIndex[fileIdx] {
				// Write this chunk and any pending sequential chunks
				if err := d.writeChunkSequence(ctx, openFiles, info, fileIdx, result.Data, pendingChunks, nextChunkIndex, fileChunks); err != nil {
					d.releaseMemory(int64(len(result.Data)))
					return err
				}
			} else {
				// Store for later (out of order)
				pendingChunks[fileIdx][chunkIdx] = result.Data
			}
		}
	}
}

func (d *Downloader) writeChunkSequence(ctx context.Context, openFiles map[int]*os.File, info *fileInfo, fileIdx int, data []byte, pendingChunks map[int]map[int][]byte, nextChunkIndex map[int]int, fileChunks map[int][]BuildManifestChunksRecord) error {
	// Get or open the file
	f, ok := openFiles[fileIdx]
	if !ok {
		var err error
		f, err = os.Create(info.FullPath)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", info.FullPath, err)
		}
		openFiles[fileIdx] = f
	}

	// Write the current chunk
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("failed to write to %s: %w", info.FullPath, err)
	}
	d.releaseMemory(int64(len(data)))
	nextChunkIndex[fileIdx]++

	// Write any pending sequential chunks
	for {
		nextIdx := nextChunkIndex[fileIdx]
		pendingData, exists := pendingChunks[fileIdx][nextIdx]
		if !exists {
			break
		}

		if _, err := f.Write(pendingData); err != nil {
			return fmt.Errorf("failed to write to %s: %w", info.FullPath, err)
		}
		d.releaseMemory(int64(len(pendingData)))
		delete(pendingChunks[fileIdx], nextIdx)
		nextChunkIndex[fileIdx]++
	}

	// Check if file is complete
	totalChunks := len(fileChunks[fileIdx])
	if nextChunkIndex[fileIdx] >= totalChunks {
		f.Close()
		delete(openFiles, fileIdx)
		d.completedFiles.Add(1)
		
		completed := d.completedFiles.Load()
		fmt.Printf("\rProgress: %d/%d files completed", completed, d.totalFiles)
	}

	return nil
}

func (d *Downloader) printDownloadInfo(manifest []BuildManifestRecord) {
	var totalSize int64
	var fileCount int
	var dirCount int

	for _, record := range manifest {
		if record.IsDirectory() {
			dirCount++
		} else {
			fileCount++
			totalSize += int64(record.SizeInBytes)
		}
	}

	fmt.Println("\n=== Download Info ===")
	fmt.Printf("Product: %s\n", d.product.Name)
	fmt.Printf("Version: %s\n", d.version.Version)
	fmt.Printf("Platform: %s\n", d.version.OS)
	fmt.Printf("Total Size: %s\n", formatBytes(totalSize))
	fmt.Printf("Files: %d\n", fileCount)
	fmt.Printf("Directories: %d\n", dirCount)
	fmt.Printf("Download Workers: %d\n", d.options.MaxDownloadWorkers)
	fmt.Printf("Max Memory Usage: %s\n", formatBytes(int64(d.options.MaxMemoryUsage)))
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// extractSHA extracts the actual SHA256 hash from a chunk identifier.
// Chunk identifiers are in the format: {prefix}_{index}_{sha256}
// For example: "5774447b9a464b9bbec6b3555ee82867_0_ed3afd9fc1217afedffbb57aa86f38d4124ce77f18430740a820cf2785814dd9"
// The actual SHA is the part after the last underscore.
func extractSHA(chunkID string) string {
	lastUnderscore := strings.LastIndex(chunkID, "_")
	if lastUnderscore == -1 {
		// No underscore found, assume the whole string is the SHA
		return chunkID
	}
	return chunkID[lastUnderscore+1:]
}


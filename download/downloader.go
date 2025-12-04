package download

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gustash/freecarnival/auth"
)

// createOptimizedClient creates an HTTP client optimized for parallel downloads
func createOptimizedClient(maxWorkers int) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			// Connection pooling - allow many connections to the same host
			MaxIdleConns:        maxWorkers * 2,
			MaxIdleConnsPerHost: maxWorkers * 2,
			MaxConnsPerHost:     maxWorkers * 2,
			IdleConnTimeout:     90 * time.Second,

			// Faster connection setup
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,

			// TLS handshake timeout
			TLSHandshakeTimeout: 10 * time.Second,

			// Disable compression (chunks are likely already compressed)
			DisableCompression: true,

			// Response header timeout
			ResponseHeaderTimeout: 30 * time.Second,

			// Expect continue timeout (for POST, but good to set)
			ExpectContinueTimeout: 1 * time.Second,

			// Force HTTP/2 where available for multiplexing
			ForceAttemptHTTP2: true,
		},
		Timeout: 0, // No overall timeout - let context handle it
	}
}

// Downloader manages parallel chunk downloads with memory limits
type Downloader struct {
	client  *http.Client
	product *auth.Product
	version *auth.ProductVersion
	options DownloadOptions

	// Memory management
	memoryUsed atomic.Int64
	memoryMu   sync.Mutex
	memoryCond *sync.Cond
	maxMemory  int64

	// Progress tracking
	totalBytes int64
	totalFiles int
	progress   *ProgressTracker
}

// NewDownloader creates a new downloader instance
func NewDownloader(client *http.Client, product *auth.Product, version *auth.ProductVersion, options DownloadOptions) *Downloader {
	// Use optimized client for downloads (ignore the passed client for actual downloads)
	optimizedClient := createOptimizedClient(options.MaxDownloadWorkers)

	d := &Downloader{
		client:    optimizedClient,
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
		if !record.IsDirectory() && !record.IsEmpty() {
			d.totalBytes += int64(record.SizeInBytes)
			d.totalFiles++
		}
	}

	if d.options.InfoOnly {
		d.printDownloadInfo(buildManifest)
		return nil
	}

	fmt.Printf("Total download size: %s (%d files)\n\n", formatBytes(d.totalBytes), d.totalFiles)

	// Create directory structure and prepare file info
	fileInfoMap, err := d.prepareInstallation(installPath, buildManifest)
	if err != nil {
		return fmt.Errorf("failed to prepare installation: %w", err)
	}

	// Group chunks by file
	fileChunks := d.groupChunksByFile(chunksManifest, fileInfoMap)

	// Create progress tracker
	d.progress = NewProgressTracker(d.totalBytes, d.totalFiles)

	// Add all files to the progress tracker
	for _, info := range fileInfoMap {
		d.progress.AddFile(info.Index, info.Record.FileName, info.ChunkCount, int64(info.Record.SizeInBytes))
	}

	// Start download workers and file writers
	err = d.downloadAndWrite(ctx, fileChunks, fileInfoMap)

	if err != nil {
		d.progress.Abort()
		return err
	}

	d.progress.Wait()
	d.progress.PrintSummary()

	// For Mac builds, mark the app bundle executables as executable
	if d.version.OS == auth.BuildOSMac {
		if err := MarkMacExecutables(installPath); err != nil {
			return fmt.Errorf("failed to mark Mac executables: %w", err)
		}
	}

	return nil
}

type fileInfo struct {
	Index      int
	Record     BuildManifestRecord
	FullPath   string
	ChunkCount int
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

func (d *Downloader) downloadAndWrite(ctx context.Context, fileChunks map[int][]BuildManifestChunksRecord, fileInfoMap map[string]*fileInfo) error {
	// Create channels
	chunkJobs := make(chan ChunkDownload, d.options.MaxDownloadWorkers*4)

	// Create per-file channels for routing downloaded chunks
	fileChannels := make(map[int]chan DownloadedChunk)
	for _, info := range fileInfoMap {
		// Buffer enough for all chunks of this file
		fileChannels[info.Index] = make(chan DownloadedChunk, len(fileChunks[info.Index])+1)
	}

	// Context for cancellation
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Error channel - buffered to avoid blocking
	errChan := make(chan error, d.options.MaxDownloadWorkers+len(fileInfoMap))

	var downloadWg sync.WaitGroup
	var writerWg sync.WaitGroup

	// Start per-file writer goroutines
	for _, info := range fileInfoMap {
		writerWg.Add(1)
		go func(info *fileInfo) {
			defer writerWg.Done()
			if err := d.singleFileWriter(ctx, info, fileChannels[info.Index], fileChunks[info.Index]); err != nil {
				select {
				case errChan <- err:
				default:
				}
				cancel()
			}
		}(info)
	}

	// Start download workers
	for i := 0; i < d.options.MaxDownloadWorkers; i++ {
		downloadWg.Add(1)
		go func() {
			defer downloadWg.Done()
			d.downloadWorkerWithRouting(ctx, chunkJobs, fileChannels)
		}()
	}

	// Feed chunk jobs - interleave chunks from different files for true parallelism
	go func() {
		defer close(chunkJobs)

		// Build a list of all file infos for round-robin scheduling
		fileInfos := make([]*fileInfo, 0, len(fileInfoMap))
		fileNames := make([]string, 0, len(fileInfoMap))
		for fileName, info := range fileInfoMap {
			fileInfos = append(fileInfos, info)
			fileNames = append(fileNames, fileName)
		}

		// Track next chunk index to send for each file
		nextChunkToSend := make([]int, len(fileInfos))
		filesRemaining := len(fileInfos)

		// Round-robin through files, sending one chunk from each
		for filesRemaining > 0 {
			for i, info := range fileInfos {
				if info == nil {
					continue // File already complete
				}

				chunks := fileChunks[info.Index]
				chunkIdx := nextChunkToSend[i]

				if chunkIdx >= len(chunks) {
					// This file is done being queued
					fileInfos[i] = nil
					filesRemaining--
					continue
				}

				select {
				case <-ctx.Done():
					return
				case chunkJobs <- ChunkDownload{
					FileIndex:  info.Index,
					ChunkIndex: chunkIdx,
					ChunkSHA:   chunks[chunkIdx].ChunkSHA,
					FilePath:   fileNames[i],
				}:
					nextChunkToSend[i]++
				}
			}
		}
	}()

	// Wait for download workers to finish, then close file channels
	go func() {
		downloadWg.Wait()
		for _, ch := range fileChannels {
			close(ch)
		}
	}()

	// Wait for all writers to finish
	writerWg.Wait()

	// Check for errors
	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (d *Downloader) downloadWorkerWithRouting(ctx context.Context, jobs <-chan ChunkDownload, fileChannels map[int]chan DownloadedChunk) {
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
			data, err := d.downloadChunk(ctx, job.FileIndex, job.ChunkSHA)

			result := DownloadedChunk{
				FileIndex:  job.FileIndex,
				ChunkIndex: job.ChunkIndex,
				Data:       data,
				Error:      err,
			}

			if err == nil && !d.options.SkipVerify {
				// Verify chunk SHA
				hash := sha256.Sum256(data)
				actualSHA := hex.EncodeToString(hash[:])
				expectedSHA := extractSHA(job.ChunkSHA)
				if actualSHA != expectedSHA {
					result.Error = fmt.Errorf("SHA mismatch for chunk %s: expected %s, got %s", job.ChunkSHA, expectedSHA, actualSHA)
					result.Data = nil
					d.releaseMemory(int64(len(data)))
				}
			}

			// Route to the appropriate file channel
			fileChan := fileChannels[job.FileIndex]
			select {
			case <-ctx.Done():
				if result.Data != nil {
					d.releaseMemory(int64(len(result.Data)))
				}
				return
			case fileChan <- result:
			}
		}
	}
}

// singleFileWriter handles writing chunks for a single file in order
func (d *Downloader) singleFileWriter(ctx context.Context, info *fileInfo, chunks <-chan DownloadedChunk, chunkManifest []BuildManifestChunksRecord) error {
	totalChunks := len(chunkManifest)
	if totalChunks == 0 {
		return nil
	}

	// Track pending chunks (out of order)
	pendingChunks := make(map[int][]byte)
	nextChunkIndex := 0

	// Open the file
	f, err := os.Create(info.FullPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", info.FullPath, err)
	}
	defer f.Close()

	// Use buffered writer for better performance (4MB buffer)
	bw := bufio.NewWriterSize(f, 4*1024*1024)
	defer bw.Flush()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-chunks:
			if !ok {
				// Channel closed - check if we got all chunks
				if nextChunkIndex < totalChunks {
					return fmt.Errorf("file %s incomplete: got %d/%d chunks", info.FullPath, nextChunkIndex, totalChunks)
				}
				// Flush buffered writer before returning
				if err := bw.Flush(); err != nil {
					return fmt.Errorf("failed to flush %s: %w", info.FullPath, err)
				}
				return nil
			}

			if result.Error != nil {
				return fmt.Errorf("download error for %s: %w", info.FullPath, result.Error)
			}

			chunkIdx := result.ChunkIndex

			if chunkIdx == nextChunkIndex {
				// Write this chunk and any pending sequential chunks
				if err := d.writeChunkBuffered(bw, result.Data, info.FullPath); err != nil {
					return err
				}
				nextChunkIndex++

				// Write any pending sequential chunks
				for {
					pendingData, exists := pendingChunks[nextChunkIndex]
					if !exists {
						break
					}
					if err := d.writeChunkBuffered(bw, pendingData, info.FullPath); err != nil {
						return err
					}
					delete(pendingChunks, nextChunkIndex)
					nextChunkIndex++
				}

				// Check if file is complete
				if nextChunkIndex >= totalChunks {
					if d.progress != nil {
						d.progress.FileComplete(info.Index)
					}
					// Flush before returning success
					if err := bw.Flush(); err != nil {
						return fmt.Errorf("failed to flush %s: %w", info.FullPath, err)
					}
					return nil
				}
			} else {
				// Store for later (out of order)
				pendingChunks[chunkIdx] = result.Data
			}
		}
	}
}

func (d *Downloader) writeChunkBuffered(bw *bufio.Writer, data []byte, filePath string) error {
	if _, err := bw.Write(data); err != nil {
		d.releaseMemory(int64(len(data)))
		return fmt.Errorf("failed to write to %s: %w", filePath, err)
	}
	chunkSize := int64(len(data))
	d.releaseMemory(chunkSize)

	// Track disk write progress
	if d.progress != nil {
		d.progress.ChunkWritten(0, chunkSize) // fileIndex not needed for overall tracking
	}
	return nil
}

func (d *Downloader) downloadChunk(ctx context.Context, fileIndex int, chunkSHA string) ([]byte, error) {
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

	// Update progress tracker
	if d.progress != nil {
		d.progress.ChunkDownloaded(fileIndex, int64(len(data)))
	}

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

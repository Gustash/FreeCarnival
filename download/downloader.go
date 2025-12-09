// Package download handles parallel chunk downloads with memory management.
package download

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/launch"
	"github.com/gustash/freecarnival/logger"
	"github.com/gustash/freecarnival/manifest"
	"github.com/gustash/freecarnival/progress"
	"github.com/gustash/freecarnival/verify"
)

// Options contains configuration for the download process.
type Options struct {
	MaxDownloadWorkers int
	MaxMemoryUsage     int
	SkipVerify         bool
	InfoOnly           bool
	Verbose            bool
}

// DefaultOptions returns download options with sensible defaults.
func DefaultOptions() Options {
	return Options{
		MaxDownloadWorkers: DefaultMaxWorkers,
		MaxMemoryUsage:     DefaultMaxMemory,
		SkipVerify:         false,
		InfoOnly:           false,
	}
}

var (
	// DefaultMaxWorkers is the default number of parallel download workers.
	DefaultMaxWorkers = min(runtime.NumCPU()*2, 16)
	// DefaultMaxMemory is the default maximum memory usage (1 GiB).
	DefaultMaxMemory = manifest.MaxChunkSize * 1024
)

// FileInfo contains metadata about a file being downloaded.
type FileInfo struct {
	Index      int
	Record     manifest.BuildRecord
	FullPath   string
	ChunkCount int
}

// ChunkJob represents a chunk to be downloaded.
type ChunkJob struct {
	FileIndex  int
	ChunkIndex int
	ChunkSHA   string
	FilePath   string
}

// ChunkResult represents a downloaded chunk.
type ChunkResult struct {
	FileIndex  int
	ChunkIndex int
	Data       []byte
	Error      error
}

// Downloader manages parallel chunk downloads with memory limits.
type Downloader struct {
	client   *http.Client
	product  *auth.Product
	version  *auth.ProductVersion
	options  Options
	memory   *MemoryLimiter
	progress *progress.Tracker

	totalBytes int64
	totalFiles int
}

// New creates a new downloader instance.
func New(client *http.Client, product *auth.Product, version *auth.ProductVersion, options Options) *Downloader {
	optimizedClient := createOptimizedClient(client, options.MaxDownloadWorkers)

	return &Downloader{
		client:  optimizedClient,
		product: product,
		version: version,
		options: options,
		memory:  NewMemoryLimiter(int64(options.MaxMemoryUsage)),
	}
}

// Download performs the full download operation.
// If there's a BuildManifest and ChunkManifests already available, those can be provided.
// Otherwise, it will fetch those manifests from the server.
func (d *Downloader) Download(ctx context.Context, installPath string, buildManifest []manifest.BuildRecord, chunksManifest []manifest.ChunkRecord) error {
	if buildManifest == nil || chunksManifest == nil {
		var manifestData []byte
		var err error

		logger.Info("Fetching build manifest...")
		buildManifest, manifestData, err = manifest.FetchBuild(ctx, d.client, d.product, d.version)
		if err != nil {
			return fmt.Errorf("failed to fetch build manifest: %w", err)
		}

		if err := auth.SaveManifest(d.product.SluggedName, d.version.Version, "manifest", manifestData); err != nil {
			logger.Warn("Failed to save manifest", "error", err)
		}

		logger.Info("Fetching chunks manifest...")
		chunksManifest, err = manifest.FetchChunks(ctx, d.client, d.product, d.version)
		if err != nil {
			return fmt.Errorf("failed to fetch chunks manifest: %w", err)
		}
	}

	d.calculateTotals(buildManifest)

	if d.options.InfoOnly {
		d.printDownloadInfo(buildManifest)
		return nil
	}

	logger.Info("Starting download", "size", progress.FormatBytes(d.totalBytes), "files", d.totalFiles)

	fileInfoMap, err := d.prepareInstallation(installPath, buildManifest)
	if err != nil {
		return fmt.Errorf("failed to prepare installation: %w", err)
	}

	fileChunks := d.groupChunksByFile(chunksManifest, fileInfoMap)

	resumeState, err := d.checkForResume(fileInfoMap, fileChunks)
	if err != nil {
		return err
	}

	if resumeState != nil {
		fileChunks = FilterChunksToDownload(fileChunks, resumeState)
		if len(fileChunks) == 0 {
			logger.Info("All files already downloaded and verified!")
			return nil
		}
	}

	d.progress = progress.New(d.totalBytes, d.totalFiles, d.options.Verbose)
	d.initializeProgress(fileInfoMap, resumeState)

	err = d.downloadAndWrite(ctx, fileChunks, fileInfoMap, resumeState)

	if ctx.Err() == context.Canceled {
		d.progress.Abort()
		logger.Info("\n\nDownload paused. Progress has been saved.")
		logger.Info("Run the same install command again to resume from where you left off.")
		return context.Canceled
	}

	if err != nil {
		d.progress.Abort()
		return err
	}

	d.progress.Wait()
	d.progress.PrintSummary()

	if d.version.OS == auth.BuildOSMac {
		if err := launch.MarkMacExecutables(installPath); err != nil {
			return fmt.Errorf("failed to mark Mac executables: %w", err)
		}
	}

	return nil
}

func (d *Downloader) calculateTotals(records []manifest.BuildRecord) {
	for _, record := range records {
		if !record.IsDirectory() && !record.IsEmpty() && record.ChangeTag != manifest.ChangeTagRemoved {
			d.totalBytes += int64(record.SizeInBytes)
			d.totalFiles++
		}
	}
}

func (d *Downloader) prepareInstallation(installPath string, records []manifest.BuildRecord) (map[string]*FileInfo, error) {
	fileInfoMap := make(map[string]*FileInfo)
	fileIndex := 0

	for _, record := range records {
		// Skip removed files
		if record.ChangeTag == manifest.ChangeTagRemoved {
			continue
		}

		fullPath := filepath.Join(installPath, record.FileName)

		if record.IsDirectory() {
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", fullPath, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create parent directory for %s: %w", fullPath, err)
		}

		if record.IsEmpty() {
			f, err := os.Create(fullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to create empty file %s: %w", fullPath, err)
			}
			f.Close()
			continue
		}

		fileInfoMap[record.FileName] = &FileInfo{
			Index:      fileIndex,
			Record:     record,
			FullPath:   fullPath,
			ChunkCount: record.Chunks,
		}
		fileIndex++
	}

	return fileInfoMap, nil
}

func (d *Downloader) groupChunksByFile(chunks []manifest.ChunkRecord, fileInfoMap map[string]*FileInfo) map[int][]manifest.ChunkRecord {
	fileChunks := make(map[int][]manifest.ChunkRecord)

	for _, chunk := range chunks {
		info, ok := fileInfoMap[chunk.FilePath]
		if !ok {
			continue
		}
		fileChunks[info.Index] = append(fileChunks[info.Index], chunk)
	}

	return fileChunks
}

func (d *Downloader) checkForResume(fileInfoMap map[string]*FileInfo, fileChunks map[int][]manifest.ChunkRecord) (*ResumeState, error) {
	if !hasExistingFiles(fileInfoMap) {
		return nil, nil
	}

	logger.Info("Found existing files, checking for resume...")
	checker := NewResumeChecker(fileInfoMap, fileChunks, d.options.MaxDownloadWorkers)
	resumeState, err := checker.CheckExistingFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to check existing files: %w", err)
	}

	for fileIndex := range resumeState.CorruptedFiles {
		for _, info := range fileInfoMap {
			if info.Index == fileIndex {
				logger.Warn("Removing corrupted file", "file", info.Record.FileName)
				os.Remove(info.FullPath)
				break
			}
		}
	}

	remainingBytes, remainingFiles := d.calculateRemaining(fileInfoMap, fileChunks, resumeState)
	logger.Info("Resuming download",
		"remaining", progress.FormatBytes(remainingBytes),
		"files", remainingFiles)
	logger.Info("Already downloaded",
		"size", progress.FormatBytes(resumeState.BytesAlreadyDownloaded),
		"files", resumeState.FilesAlreadyComplete)

	return resumeState, nil
}

func (d *Downloader) calculateRemaining(fileInfoMap map[string]*FileInfo, fileChunks map[int][]manifest.ChunkRecord, resumeState *ResumeState) (int64, int) {
	var remainingBytes int64
	var remainingFiles int

	for fileIndex := range fileChunks {
		if resumeState.CompletedFiles[fileIndex] {
			continue
		}

		for _, info := range fileInfoMap {
			if info.Index != fileIndex {
				continue
			}
			remainingFiles++
			startChunk := resumeState.StartChunkIndex[fileIndex]
			totalChunks := info.ChunkCount

			for i := startChunk; i < totalChunks; i++ {
				if i == totalChunks-1 {
					lastChunkSize := int64(info.Record.SizeInBytes) - int64(totalChunks-1)*manifest.MaxChunkSize
					if lastChunkSize > 0 {
						remainingBytes += lastChunkSize
					}
				} else {
					remainingBytes += manifest.MaxChunkSize
				}
			}
			break
		}
	}

	return remainingBytes, remainingFiles
}

func (d *Downloader) initializeProgress(fileInfoMap map[string]*FileInfo, resumeState *ResumeState) {
	for _, info := range fileInfoMap {
		chunksAlreadyWritten := 0
		if resumeState != nil {
			chunksAlreadyWritten = resumeState.StartChunkIndex[info.Index]
		}
		d.progress.AddFile(info.Index, info.Record.FileName, info.ChunkCount, int64(info.Record.SizeInBytes), chunksAlreadyWritten)
	}

	if resumeState != nil {
		for fileIndex := range resumeState.CompletedFiles {
			d.progress.FileComplete(fileIndex)
		}
		d.progress.AddDownloadedBytes(resumeState.BytesAlreadyDownloaded)
	}
}

func (d *Downloader) downloadAndWrite(ctx context.Context, fileChunks map[int][]manifest.ChunkRecord, fileInfoMap map[string]*FileInfo, resumeState *ResumeState) error {
	chunkJobs := make(chan ChunkJob, d.options.MaxDownloadWorkers)
	downloadedChunks := make(chan ChunkResult, d.memory.maxMemory/manifest.MaxChunkSize)

	fileIndexToInfo := make(map[int]*FileInfo)
	fileIndexToChunks := make(map[int]int)
	for _, info := range fileInfoMap {
		fileIndexToInfo[info.Index] = info
		fileIndexToChunks[info.Index] = len(fileChunks[info.Index])
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var downloadWg sync.WaitGroup

	chunkDownloader := NewChunkDownloader(d.client, d.product, d.version.OS, d.progress)

	for i := 0; i < d.options.MaxDownloadWorkers; i++ {
		downloadWg.Add(1)
		go func() {
			defer downloadWg.Done()
			d.downloadWorker(ctx, chunkDownloader, chunkJobs, downloadedChunks)
		}()
	}

	writerDone := make(chan error, 1)
	diskWriter := NewDiskWriter(d.memory, d.progress)
	go func() {
		writerDone <- diskWriter.WriteChunks(ctx, downloadedChunks, fileIndexToInfo, fileIndexToChunks, resumeState)
	}()

	go d.feedChunkJobs(ctx, chunkJobs, fileChunks, fileInfoMap, resumeState)

	go func() {
		downloadWg.Wait()
		close(downloadedChunks)
	}()

	if err := <-writerDone; err != nil {
		cancel()
		return err
	}

	return nil
}

func (d *Downloader) downloadWorker(ctx context.Context, downloader *ChunkDownloader, jobs <-chan ChunkJob, results chan<- ChunkResult) {
	for job := range jobs {
		if !d.memory.Acquire(ctx, manifest.MaxChunkSize) {
			return
		}

		data, err := downloader.Download(ctx, job.FileIndex, job.ChunkSHA)

		if err == context.Canceled {
			d.memory.Release(manifest.MaxChunkSize)
			return
		}

		if err != nil {
			d.memory.Release(manifest.MaxChunkSize)
			results <- ChunkResult{
				FileIndex:  job.FileIndex,
				ChunkIndex: job.ChunkIndex,
				Error:      err,
			}
			continue
		}

		if !d.options.SkipVerify {
			expectedSHA := manifest.ExtractSHA(job.ChunkSHA)
			if !verify.Chunk(data, expectedSHA) {
				d.memory.Release(manifest.MaxChunkSize)
				results <- ChunkResult{
					FileIndex:  job.FileIndex,
					ChunkIndex: job.ChunkIndex,
					Error:      fmt.Errorf("SHA mismatch for chunk %s", job.ChunkSHA),
				}
				continue
			}
		}

		results <- ChunkResult{
			FileIndex:  job.FileIndex,
			ChunkIndex: job.ChunkIndex,
			Data:       data,
		}
	}
}

func (d *Downloader) feedChunkJobs(ctx context.Context, chunkJobs chan<- ChunkJob, fileChunks map[int][]manifest.ChunkRecord, fileInfoMap map[string]*FileInfo, resumeState *ResumeState) {
	defer close(chunkJobs)

	type fileEntry struct {
		info     *FileInfo
		fileName string
	}

	var files []fileEntry
	for fileName, info := range fileInfoMap {
		if resumeState != nil && resumeState.CompletedFiles[info.Index] {
			continue
		}
		files = append(files, fileEntry{info: info, fileName: fileName})
	}

	nextChunkToSend := make([]int, len(files))
	filesRemaining := len(files)

	for filesRemaining > 0 {
		for i, fe := range files {
			if fe.info == nil {
				continue
			}

			chunks := fileChunks[fe.info.Index]
			chunkIdx := nextChunkToSend[i]

			if chunkIdx >= len(chunks) {
				files[i].info = nil
				filesRemaining--
				continue
			}

			select {
			case <-ctx.Done():
				return
			case chunkJobs <- ChunkJob{
				FileIndex:  fe.info.Index,
				ChunkIndex: chunkIdx,
				ChunkSHA:   chunks[chunkIdx].ChunkSHA,
				FilePath:   fe.fileName,
			}:
				nextChunkToSend[i]++
			}
		}
	}
}

func (d *Downloader) printDownloadInfo(records []manifest.BuildRecord) {
	var totalSize int64
	var fileCount int
	var dirCount int

	for _, record := range records {
		if record.IsDirectory() {
			dirCount++
		} else {
			fileCount++
			totalSize += int64(record.SizeInBytes)
		}
	}

	// This is user-facing formatted output, keep using fmt
	fmt.Println("\n=== Download Info ===")
	fmt.Printf("Product: %s\n", d.product.Name)
	fmt.Printf("Version: %s\n", d.version.Version)
	fmt.Printf("Platform: %s\n", d.version.OS)
	fmt.Printf("Total Size: %s\n", progress.FormatBytes(totalSize))
	fmt.Printf("Files: %d\n", fileCount)
	fmt.Printf("Directories: %d\n", dirCount)
	fmt.Printf("Download Workers: %d\n", d.options.MaxDownloadWorkers)
	fmt.Printf("Max Memory Usage: %s\n", progress.FormatBytes(int64(d.options.MaxMemoryUsage)))
}

// createOptimizedClient optimizes HTTP transport for parallel chunk downloads:
// - Increases MaxIdleConnsPerHost to enable connection reuse across workers
// - Disables compression to save CPU (game files aren't gzipped)
// - Forces HTTP/2 for better multiplexing when CDN supports it
func createOptimizedClient(client *http.Client, maxWorkers int) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	client.Transport = &http.Transport{
		MaxIdleConnsPerHost: maxWorkers,
		DisableCompression:  true,
		ForceAttemptHTTP2:   true,
	}

	return client
}

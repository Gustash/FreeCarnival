package download

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
)

// ResumeState tracks which files/chunks need to be downloaded
type ResumeState struct {
	// Files that are fully downloaded and verified
	CompletedFiles map[int]bool
	// For each file, the starting chunk index (0 = start from beginning)
	StartChunkIndex map[int]int
	// Files that need to be re-downloaded from scratch (corruption detected)
	CorruptedFiles map[int]bool
	// Bytes already downloaded (for progress tracking)
	BytesAlreadyDownloaded int64
	// Files already complete
	FilesAlreadyComplete int
}

// ResumeChecker checks existing files and determines what needs to be downloaded
type ResumeChecker struct {
	installPath string
	fileInfoMap map[string]*fileInfo
	fileChunks  map[int][]BuildManifestChunksRecord // fileIndex -> chunks
	maxWorkers  int
}

// checkResult holds the result of checking a single file
type checkResult struct {
	fileIndex   int
	isComplete  bool
	startChunk  int
	isCorrupted bool
	bytesValid  int64
	err         error
}

// NewResumeChecker creates a new resume checker
func NewResumeChecker(installPath string, fileInfoMap map[string]*fileInfo, fileChunks map[int][]BuildManifestChunksRecord, maxWorkers int) *ResumeChecker {
	return &ResumeChecker{
		installPath: installPath,
		fileInfoMap: fileInfoMap,
		fileChunks:  fileChunks,
		maxWorkers:  maxWorkers,
	}
}

// CheckExistingFiles checks all existing files in parallel and returns the resume state
func (rc *ResumeChecker) CheckExistingFiles() (*ResumeState, error) {
	state := &ResumeState{
		CompletedFiles:  make(map[int]bool),
		StartChunkIndex: make(map[int]int),
		CorruptedFiles:  make(map[int]bool),
	}

	// Build list of files to check
	type fileCheck struct {
		fileName string
		info     *fileInfo
	}
	var filesToCheck []fileCheck
	for fileName, info := range rc.fileInfoMap {
		filesToCheck = append(filesToCheck, fileCheck{fileName: fileName, info: info})
	}

	if len(filesToCheck) == 0 {
		return state, nil
	}

	// Check files in parallel
	workCh := make(chan fileCheck, len(filesToCheck))
	resultCh := make(chan checkResult, len(filesToCheck))

	// Track progress
	var checked atomic.Int64
	total := int64(len(filesToCheck))

	// Start workers
	var wg sync.WaitGroup
	numWorkers := rc.maxWorkers
	if numWorkers > len(filesToCheck) {
		numWorkers = len(filesToCheck)
	}

	// Mutex for clean progress output
	var progressMu sync.Mutex

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for fc := range workCh {
				result := rc.checkFile(fc.info)
				resultCh <- result

				count := checked.Add(1)
				progressMu.Lock()
				fmt.Printf("\rChecking existing files... %d/%d", count, total)
				progressMu.Unlock()
			}
		}()
	}

	// Send work
	for _, fc := range filesToCheck {
		workCh <- fc
	}
	close(workCh)

	// Wait for completion
	wg.Wait()
	close(resultCh)

	fmt.Println() // New line after progress

	// Collect results
	var partialFiles, newFiles, corruptedFiles int
	for result := range resultCh {
		if result.err != nil {
			return nil, result.err
		}

		if result.isComplete {
			state.CompletedFiles[result.fileIndex] = true
			state.BytesAlreadyDownloaded += result.bytesValid
			state.FilesAlreadyComplete++
		} else if result.isCorrupted {
			state.CorruptedFiles[result.fileIndex] = true
			state.StartChunkIndex[result.fileIndex] = 0
			corruptedFiles++
		} else if result.startChunk > 0 {
			// Partial file with some valid chunks
			state.StartChunkIndex[result.fileIndex] = result.startChunk
			state.BytesAlreadyDownloaded += result.bytesValid
			partialFiles++
		} else {
			// New file or file with no valid chunks
			state.StartChunkIndex[result.fileIndex] = 0
			newFiles++
		}
	}

	fmt.Printf("\nResume analysis: %d complete, %d partial, %d new, %d corrupted\n",
		state.FilesAlreadyComplete, partialFiles, newFiles, corruptedFiles)

	return state, nil
}

// checkFile checks a single file and determines its state
func (rc *ResumeChecker) checkFile(info *fileInfo) checkResult {
	result := checkResult{
		fileIndex: info.Index,
	}

	// Check if file exists
	stat, err := os.Stat(info.FullPath)
	if os.IsNotExist(err) {
		// File doesn't exist, need to download from start
		result.startChunk = 0
		return result
	}
	if err != nil {
		result.err = fmt.Errorf("failed to stat %s: %w", info.FullPath, err)
		return result
	}

	fileSize := stat.Size()
	expectedSize := int64(info.Record.SizeInBytes)

	// If file is complete size, verify the whole file
	if fileSize == expectedSize {
		hash, err := hashFile(info.FullPath)
		if err != nil {
			result.err = fmt.Errorf("failed to hash %s: %w", info.FullPath, err)
			return result
		}

		if hash == info.Record.SHA {
			result.isComplete = true
			result.bytesValid = expectedSize
			return result
		}

		// Hash mismatch - file is corrupted, re-download entirely
		result.isCorrupted = true
		return result
	}

	// File is partial - check chunks
	chunks := rc.fileChunks[info.Index]
	if len(chunks) == 0 {
		result.startChunk = 0
		return result
	}

	// Calculate how many complete chunks we have based on file size
	completeChunks := int(fileSize / MaxChunkSize)
	remainderBytes := fileSize % MaxChunkSize

	// If there's a remainder, we have a partial chunk that we need to discard
	// (we'll re-download from the start of that chunk)
	if remainderBytes > 0 {
		// Truncate file to complete chunks only
		if completeChunks > 0 {
			if err := os.Truncate(info.FullPath, int64(completeChunks)*MaxChunkSize); err != nil {
				// Can't truncate, re-download entirely
				result.isCorrupted = true
				return result
			}
		} else {
			// No complete chunks, delete and start over
			os.Remove(info.FullPath)
			result.startChunk = 0
			return result
		}
	}

	// Verify chunks from the beginning, find last valid chunk
	file, err := os.Open(info.FullPath)
	if err != nil {
		result.isCorrupted = true
		return result
	}
	defer file.Close()

	lastValidChunk := -1
	for i := 0; i < completeChunks && i < len(chunks); i++ {
		chunkData := make([]byte, MaxChunkSize)
		n, err := io.ReadFull(file, chunkData)
		if err != nil && err != io.ErrUnexpectedEOF {
			// Can't read chunk, stop here
			break
		}
		chunkData = chunkData[:n]

		expectedSHA := chunks[i].ChunkSHA
		if !VerifyChunk(chunkData, expectedSHA) {
			// Chunk is corrupted - stop here, resume from this chunk
			break
		}
		lastValidChunk = i
	}

	if lastValidChunk < 0 {
		// No valid chunks at all - re-download entire file
		os.Remove(info.FullPath)
		result.startChunk = 0
		return result
	}

	// Truncate file to last valid chunk
	validBytes := int64(lastValidChunk+1) * MaxChunkSize
	if err := os.Truncate(info.FullPath, validBytes); err != nil {
		// Can't truncate, re-download entirely
		os.Remove(info.FullPath)
		result.startChunk = 0
		return result
	}

	// Continue from the chunk after the last valid one
	result.startChunk = lastValidChunk + 1
	result.bytesValid = validBytes

	return result
}

// FilterChunksToDownload filters the chunk list based on resume state
func FilterChunksToDownload(fileChunks map[int][]BuildManifestChunksRecord, state *ResumeState) map[int][]BuildManifestChunksRecord {
	filtered := make(map[int][]BuildManifestChunksRecord)

	for fileIndex, chunks := range fileChunks {
		// Skip completed files
		if state.CompletedFiles[fileIndex] {
			continue
		}

		// For corrupted files, include all chunks
		if state.CorruptedFiles[fileIndex] {
			filtered[fileIndex] = chunks
			continue
		}

		// For partial files, skip already-downloaded chunks
		startChunk := state.StartChunkIndex[fileIndex]
		if startChunk < len(chunks) {
			filtered[fileIndex] = chunks[startChunk:]
		}
	}

	return filtered
}

// hasExistingFiles checks if any files already exist in the install directory
func hasExistingFiles(fileInfoMap map[string]*fileInfo) bool {
	for _, info := range fileInfoMap {
		if _, err := os.Stat(info.FullPath); err == nil {
			return true
		}
	}
	return false
}

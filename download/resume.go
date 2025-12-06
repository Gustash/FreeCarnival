package download

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/gustash/freecarnival/manifest"
	"github.com/gustash/freecarnival/verify"
)

// ResumeState tracks which files/chunks need to be downloaded.
type ResumeState struct {
	CompletedFiles         map[int]bool
	StartChunkIndex        map[int]int
	CorruptedFiles         map[int]bool
	BytesAlreadyDownloaded int64
	FilesAlreadyComplete   int
}

// ResumeChecker checks existing files and determines what needs to be downloaded.
type ResumeChecker struct {
	fileInfoMap map[string]*FileInfo
	fileChunks  map[int][]manifest.ChunkRecord
	maxWorkers  int
}

type checkResult struct {
	fileIndex   int
	isComplete  bool
	startChunk  int
	isCorrupted bool
	bytesValid  int64
	err         error
}

// NewResumeChecker creates a new resume checker.
func NewResumeChecker(fileInfoMap map[string]*FileInfo, fileChunks map[int][]manifest.ChunkRecord, maxWorkers int) *ResumeChecker {
	return &ResumeChecker{
		fileInfoMap: fileInfoMap,
		fileChunks:  fileChunks,
		maxWorkers:  maxWorkers,
	}
}

// CheckExistingFiles checks all existing files in parallel and returns the resume state.
func (rc *ResumeChecker) CheckExistingFiles() (*ResumeState, error) {
	state := &ResumeState{
		CompletedFiles:  make(map[int]bool),
		StartChunkIndex: make(map[int]int),
		CorruptedFiles:  make(map[int]bool),
	}

	var filesToCheck []*FileInfo
	for _, info := range rc.fileInfoMap {
		filesToCheck = append(filesToCheck, info)
	}

	if len(filesToCheck) == 0 {
		return state, nil
	}

	workCh := make(chan *FileInfo, len(filesToCheck))
	resultCh := make(chan checkResult, len(filesToCheck))

	var checked atomic.Int64
	total := int64(len(filesToCheck))

	var wg sync.WaitGroup
	numWorkers := rc.maxWorkers
	if numWorkers > len(filesToCheck) {
		numWorkers = len(filesToCheck)
	}

	var progressMu sync.Mutex

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for info := range workCh {
				result := rc.checkFile(info)
				resultCh <- result

				count := checked.Add(1)
				progressMu.Lock()
				fmt.Printf("\rChecking existing files... %d/%d", count, total)
				progressMu.Unlock()
			}
		}()
	}

	for _, info := range filesToCheck {
		workCh <- info
	}
	close(workCh)

	wg.Wait()
	close(resultCh)

	fmt.Println()

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
			state.StartChunkIndex[result.fileIndex] = result.startChunk
			state.BytesAlreadyDownloaded += result.bytesValid
			partialFiles++
		} else {
			state.StartChunkIndex[result.fileIndex] = 0
			newFiles++
		}
	}

	fmt.Printf("\nResume analysis: %d complete, %d partial, %d new, %d corrupted\n",
		state.FilesAlreadyComplete, partialFiles, newFiles, corruptedFiles)

	return state, nil
}

func (rc *ResumeChecker) checkFile(info *FileInfo) checkResult {
	result := checkResult{fileIndex: info.Index}

	stat, err := os.Stat(info.FullPath)
	if os.IsNotExist(err) {
		result.startChunk = 0
		return result
	}
	if err != nil {
		result.err = fmt.Errorf("failed to stat %s: %w", info.FullPath, err)
		return result
	}

	fileSize := stat.Size()
	expectedSize := int64(info.Record.SizeInBytes)

	if fileSize == expectedSize {
		hash, err := verify.HashFile(info.FullPath)
		if err != nil {
			result.err = fmt.Errorf("failed to hash %s: %w", info.FullPath, err)
			return result
		}

		if hash == info.Record.SHA {
			result.isComplete = true
			result.bytesValid = expectedSize
			return result
		}

		result.isCorrupted = true
		return result
	}

	return rc.checkPartialFile(info, fileSize)
}

func (rc *ResumeChecker) checkPartialFile(info *FileInfo, fileSize int64) checkResult {
	result := checkResult{fileIndex: info.Index}

	chunks := rc.fileChunks[info.Index]
	if len(chunks) == 0 {
		result.startChunk = 0
		return result
	}

	completeChunks := int(fileSize / manifest.MaxChunkSize)
	remainderBytes := fileSize % manifest.MaxChunkSize

	if remainderBytes > 0 {
		if completeChunks > 0 {
			if err := os.Truncate(info.FullPath, int64(completeChunks)*manifest.MaxChunkSize); err != nil {
				result.isCorrupted = true
				return result
			}
		} else {
			os.Remove(info.FullPath)
			result.startChunk = 0
			return result
		}
	}

	lastValidChunk := rc.findLastValidChunk(info.FullPath, chunks, completeChunks)

	if lastValidChunk < 0 {
		os.Remove(info.FullPath)
		result.startChunk = 0
		return result
	}

	validBytes := int64(lastValidChunk+1) * manifest.MaxChunkSize
	if err := os.Truncate(info.FullPath, validBytes); err != nil {
		os.Remove(info.FullPath)
		result.startChunk = 0
		return result
	}

	result.startChunk = lastValidChunk + 1
	result.bytesValid = validBytes

	return result
}

func (rc *ResumeChecker) findLastValidChunk(filePath string, chunks []manifest.ChunkRecord, completeChunks int) int {
	file, err := os.Open(filePath)
	if err != nil {
		return -1
	}
	defer file.Close()

	lastValidChunk := -1
	for i := 0; i < completeChunks && i < len(chunks); i++ {
		chunkData := make([]byte, manifest.MaxChunkSize)
		n, err := io.ReadFull(file, chunkData)
		if err != nil && err != io.ErrUnexpectedEOF {
			break
		}
		chunkData = chunkData[:n]

		expectedSHA := manifest.ExtractSHA(chunks[i].ChunkSHA)
		if !verify.Chunk(chunkData, expectedSHA) {
			break
		}
		lastValidChunk = i
	}

	return lastValidChunk
}

// FilterChunksToDownload filters the chunk list based on resume state.
func FilterChunksToDownload(fileChunks map[int][]manifest.ChunkRecord, state *ResumeState) map[int][]manifest.ChunkRecord {
	filtered := make(map[int][]manifest.ChunkRecord)

	for fileIndex, chunks := range fileChunks {
		if state.CompletedFiles[fileIndex] {
			continue
		}

		if state.CorruptedFiles[fileIndex] {
			filtered[fileIndex] = chunks
			continue
		}

		startChunk := state.StartChunkIndex[fileIndex]
		if startChunk < len(chunks) {
			filtered[fileIndex] = chunks[startChunk:]
		}
	}

	return filtered
}

func hasExistingFiles(fileInfoMap map[string]*FileInfo) bool {
	for _, info := range fileInfoMap {
		if _, err := os.Stat(info.FullPath); err == nil {
			return true
		}
	}
	return false
}

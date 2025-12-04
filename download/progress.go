package download

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProgressTracker manages download progress display
type ProgressTracker struct {
	// Overall stats
	totalBytes      int64
	downloadedBytes atomic.Int64
	writtenBytes    atomic.Int64
	totalFiles      int
	completedFiles  atomic.Int32

	// Speed tracking
	lastDownloadedBytes int64
	lastWrittenBytes    int64
	lastSpeedUpdate     time.Time
	downloadSpeed       atomic.Int64 // bytes per second
	diskSpeed           atomic.Int64 // bytes per second

	// File tracking
	files   map[int]*fileProgress
	filesMu sync.RWMutex

	// Display control
	stopChan   chan struct{}
	doneChan   chan struct{}
	linesDrawn int
}

type fileProgress struct {
	fileName      string
	totalChunks   int
	totalSize     int64
	chunksWritten atomic.Int32
	bytesWritten  atomic.Int64
	complete      bool
}

// NewProgressTracker creates a new progress tracker
func NewProgressTracker(totalBytes int64, totalFiles int) *ProgressTracker {
	pt := &ProgressTracker{
		totalBytes:      totalBytes,
		totalFiles:      totalFiles,
		files:           make(map[int]*fileProgress),
		lastSpeedUpdate: time.Now(),
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
	}

	// Start the display loop
	go pt.displayLoop()

	return pt
}

func (pt *ProgressTracker) displayLoop() {
	defer close(pt.doneChan)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pt.stopChan:
			pt.render() // Final render
			return
		case <-ticker.C:
			pt.updateSpeed()
			pt.render()
		}
	}
}

func (pt *ProgressTracker) updateSpeed() {
	now := time.Now()
	elapsed := now.Sub(pt.lastSpeedUpdate).Seconds()
	if elapsed > 0 {
		currentDownloaded := pt.downloadedBytes.Load()
		currentWritten := pt.writtenBytes.Load()

		downloadDelta := currentDownloaded - pt.lastDownloadedBytes
		writtenDelta := currentWritten - pt.lastWrittenBytes

		pt.downloadSpeed.Store(int64(float64(downloadDelta) / elapsed))
		pt.diskSpeed.Store(int64(float64(writtenDelta) / elapsed))

		pt.lastDownloadedBytes = currentDownloaded
		pt.lastWrittenBytes = currentWritten
		pt.lastSpeedUpdate = now
	}
}

func (pt *ProgressTracker) render() {
	// Move cursor up to overwrite previous output
	if pt.linesDrawn > 0 {
		fmt.Fprintf(os.Stdout, "\033[%dA", pt.linesDrawn)
	}

	var lines []string

	// Overall progress
	downloaded := pt.downloadedBytes.Load()
	percent := float64(0)
	if pt.totalBytes > 0 {
		percent = float64(downloaded) / float64(pt.totalBytes) * 100
	}

	dlSpeed := pt.downloadSpeed.Load()
	dskSpeed := pt.diskSpeed.Load()
	completed := pt.completedFiles.Load()

	// File list (sorted by index for stable display)
	pt.filesMu.RLock()
	indices := make([]int, 0, len(pt.files))
	for idx := range pt.files {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	for _, idx := range indices {
		fp := pt.files[idx]
		chunksWritten := fp.chunksWritten.Load()
		filePercent := float64(0)
		if fp.totalChunks > 0 {
			filePercent = float64(chunksWritten) / float64(fp.totalChunks) * 100
		}

		// Status indicator
		status := "⏳"
		if fp.complete {
			status = "✓ "
		}

		// Truncate filename
		name := fp.fileName
		if len(name) > 45 {
			name = "..." + name[len(name)-42:]
		}

		// Mini progress bar for file
		fileBarWidth := 20
		fileFilled := int(filePercent / 100 * float64(fileBarWidth))
		if fileFilled < 0 {
			fileFilled = 0
		}
		if fileFilled > fileBarWidth {
			fileFilled = fileBarWidth
		}
		fileBar := strings.Repeat("█", fileFilled) + strings.Repeat("░", fileBarWidth-fileFilled)

		lines = append(lines, fmt.Sprintf(
			"\033[K%s %-45s [%s] %5.1f%%",
			status,
			name,
			fileBar,
			filePercent,
		))
	}
	pt.filesMu.RUnlock()

	lines = append(lines, "\033[K") // Empty separator line

	// Progress bar at the bottom
	barWidth := 60
	filled := int(percent / 100 * float64(barWidth))
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	lines = append(lines, fmt.Sprintf("\033[K[%s]", bar))

	// Stats line
	lines = append(lines, fmt.Sprintf(
		"\033[K%s / %s [%5.1f%%] | DL: %s/s | Disk: %s/s | Files: %d/%d",
		formatBytes(downloaded),
		formatBytes(pt.totalBytes),
		percent,
		formatBytes(dlSpeed),
		formatBytes(dskSpeed),
		completed,
		pt.totalFiles,
	))

	// Clear any extra lines from previous render
	for i := len(lines); i < pt.linesDrawn; i++ {
		lines = append(lines, "\033[K")
	}

	// Print all lines
	output := strings.Join(lines, "\n") + "\n"
	fmt.Fprint(os.Stdout, output)

	pt.linesDrawn = len(lines)
}

// AddFile adds a file to track
func (pt *ProgressTracker) AddFile(fileIndex int, fileName string, totalChunks int, fileSize int64) {
	pt.filesMu.Lock()
	defer pt.filesMu.Unlock()

	pt.files[fileIndex] = &fileProgress{
		fileName:    fileName,
		totalChunks: totalChunks,
		totalSize:   fileSize,
	}
}

// ChunkDownloaded records that a chunk was downloaded
func (pt *ProgressTracker) ChunkDownloaded(fileIndex int, chunkSize int64) {
	pt.downloadedBytes.Add(chunkSize)
}

// ChunkWritten records that a chunk was written to disk
func (pt *ProgressTracker) ChunkWritten(fileIndex int, chunkSize int64) {
	pt.writtenBytes.Add(chunkSize)

	pt.filesMu.RLock()
	fp, ok := pt.files[fileIndex]
	pt.filesMu.RUnlock()

	if ok {
		fp.chunksWritten.Add(1)
		fp.bytesWritten.Add(chunkSize)
	}
}

// FileComplete marks a file as complete
func (pt *ProgressTracker) FileComplete(fileIndex int) {
	pt.completedFiles.Add(1)

	pt.filesMu.Lock()
	if fp, ok := pt.files[fileIndex]; ok {
		fp.complete = true
		// Ensure it shows 100%
		fp.chunksWritten.Store(int32(fp.totalChunks))
	}
	pt.filesMu.Unlock()
}

// GetStats returns current download statistics
func (pt *ProgressTracker) GetStats() (downloadSpeed, diskSpeed int64, completed, total int) {
	return pt.downloadSpeed.Load(), pt.diskSpeed.Load(), int(pt.completedFiles.Load()), pt.totalFiles
}

// Wait waits for the progress display to finish
func (pt *ProgressTracker) Wait() {
	select {
	case <-pt.stopChan:
		// Already stopped
	default:
		close(pt.stopChan)
	}
	<-pt.doneChan
}

// PrintSummary prints a final summary
func (pt *ProgressTracker) PrintSummary() {
	fmt.Printf("\n✓ Download complete: %d files, %s\n",
		pt.completedFiles.Load(),
		formatBytes(pt.totalBytes),
	)
}

// Abort stops the progress display
func (pt *ProgressTracker) Abort() {
	pt.Wait()
}

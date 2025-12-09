// Package progress provides download progress tracking and display.
package progress

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Tracker manages download progress display.
type Tracker struct {
	totalBytes      int64
	downloadedBytes atomic.Int64
	writtenBytes    atomic.Int64
	totalFiles      int
	completedFiles  atomic.Int32

	lastDownloadedBytes int64
	lastWrittenBytes    int64
	lastSpeedUpdate     time.Time
	downloadSpeed       atomic.Int64
	diskSpeed           atomic.Int64

	files   map[int]*fileProgress
	filesMu sync.RWMutex

	stopChan   chan struct{}
	doneChan   chan struct{}
	linesDrawn int
	verbose    bool
}

type fileProgress struct {
	fileName      string
	totalChunks   int
	totalSize     int64
	chunksWritten atomic.Int32
	bytesWritten  atomic.Int64
	complete      bool
}

// New creates a new progress tracker.
func New(totalBytes int64, totalFiles int, verbose bool) *Tracker {
	pt := &Tracker{
		totalBytes:      totalBytes,
		totalFiles:      totalFiles,
		files:           make(map[int]*fileProgress),
		lastSpeedUpdate: time.Now(),
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
		verbose:         verbose,
	}

	go pt.displayLoop()

	return pt
}

func (pt *Tracker) displayLoop() {
	defer close(pt.doneChan)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pt.stopChan:
			pt.render()
			return
		case <-ticker.C:
			pt.updateSpeed()
			pt.render()
		}
	}
}

func (pt *Tracker) updateSpeed() {
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

func (pt *Tracker) render() {
	if pt.linesDrawn > 0 {
		fmt.Fprintf(os.Stdout, "\033[%dA", pt.linesDrawn)
	}

	var lines []string

	downloaded := pt.downloadedBytes.Load()
	percent := float64(0)
	if pt.totalBytes > 0 {
		percent = float64(downloaded) / float64(pt.totalBytes) * 100
	}

	dlSpeed := pt.downloadSpeed.Load()
	dskSpeed := pt.diskSpeed.Load()
	completed := pt.completedFiles.Load()

	if pt.verbose {
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

			status := "⏳"
			if fp.complete {
				status = "✓ "
			}

			name := fp.fileName
			if len(name) > 45 {
				name = "..." + name[len(name)-42:]
			}

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

		lines = append(lines, "\033[K")
	}

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

	lines = append(lines, fmt.Sprintf(
		"\033[K%s / %s [%5.1f%%] | DL: %s/s | Disk: %s/s | Files: %d/%d",
		FormatBytes(downloaded),
		FormatBytes(pt.totalBytes),
		percent,
		FormatBytes(dlSpeed),
		FormatBytes(dskSpeed),
		completed,
		pt.totalFiles,
	))

	for i := len(lines); i < pt.linesDrawn; i++ {
		lines = append(lines, "\033[K")
	}

	output := strings.Join(lines, "\n") + "\n"
	fmt.Fprint(os.Stdout, output)

	pt.linesDrawn = len(lines)
}

// AddFile adds a file to track.
func (pt *Tracker) AddFile(fileIndex int, fileName string, totalChunks int, fileSize int64, chunksAlreadyWritten int) {
	pt.filesMu.Lock()
	defer pt.filesMu.Unlock()

	fp := &fileProgress{
		fileName:    fileName,
		totalChunks: totalChunks,
		totalSize:   fileSize,
	}
	fp.chunksWritten.Store(int32(chunksAlreadyWritten))
	pt.files[fileIndex] = fp
}

// ChunkDownloaded records that a chunk was downloaded.
func (pt *Tracker) ChunkDownloaded(fileIndex int, chunkSize int64) {
	pt.downloadedBytes.Add(chunkSize)
}

// AddDownloadedBytes adds already-downloaded bytes (for resume).
func (pt *Tracker) AddDownloadedBytes(bytes int64) {
	pt.downloadedBytes.Add(bytes)
	pt.writtenBytes.Add(bytes)
}

// ChunkWritten records that a chunk was written to disk.
func (pt *Tracker) ChunkWritten(fileIndex int, chunkSize int64) {
	pt.writtenBytes.Add(chunkSize)

	pt.filesMu.RLock()
	fp, ok := pt.files[fileIndex]
	pt.filesMu.RUnlock()

	if ok {
		fp.chunksWritten.Add(1)
		fp.bytesWritten.Add(chunkSize)
	}
}

// FileComplete marks a file as complete.
func (pt *Tracker) FileComplete(fileIndex int) {
	pt.completedFiles.Add(1)

	pt.filesMu.Lock()
	if fp, ok := pt.files[fileIndex]; ok {
		fp.complete = true
		fp.chunksWritten.Store(int32(fp.totalChunks))
	}
	pt.filesMu.Unlock()
}

// GetStats returns current download statistics.
func (pt *Tracker) GetStats() (downloadSpeed, diskSpeed int64, completed, total int) {
	return pt.downloadSpeed.Load(), pt.diskSpeed.Load(), int(pt.completedFiles.Load()), pt.totalFiles
}

// Wait waits for the progress display to finish.
func (pt *Tracker) Wait() {
	select {
	case <-pt.stopChan:
	default:
		close(pt.stopChan)
	}
	<-pt.doneChan
}

// PrintSummary prints a final summary.
func (pt *Tracker) PrintSummary() {
	fmt.Printf("\n✓ Download complete: %d files, %s\n",
		pt.completedFiles.Load(),
		FormatBytes(pt.totalBytes),
	)
}

// Abort stops the progress display.
func (pt *Tracker) Abort() {
	pt.Wait()
}

// FormatBytes formats bytes in human-readable form.
func FormatBytes(bytes int64) string {
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


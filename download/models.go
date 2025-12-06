package download

import (
	"github.com/gustash/freecarnival/auth"
)

// BuildManifestRecord represents a file entry in the build manifest CSV
type BuildManifestRecord struct {
	SizeInBytes int    `csv:"Size in Bytes"`
	Chunks      int    `csv:"Chunks"`
	SHA         string `csv:"SHA"`
	Flags       int    `csv:"Flags"`
	FileName    string `csv:"File Name"`
	ChangeTag   string `csv:"Change Tag"`
}

// IsDirectory returns true if this record represents a directory
func (r *BuildManifestRecord) IsDirectory() bool {
	return r.Flags == 40
}

// IsEmpty returns true if the file has no content
func (r *BuildManifestRecord) IsEmpty() bool {
	return r.SizeInBytes == 0
}

// BuildManifestChunksRecord represents a chunk entry in the chunks manifest CSV
type BuildManifestChunksRecord struct {
	ID       int    `csv:"ID"`
	FilePath string `csv:"Filepath"`
	ChunkSHA string `csv:"Chunk SHA"`
}

// DownloadOptions contains configuration for the download process
type DownloadOptions struct {
	// MaxDownloadWorkers is the number of parallel download workers
	MaxDownloadWorkers int
	// MaxMemoryUsage is the maximum memory to use for buffering chunks
	MaxMemoryUsage int
	// SkipVerify skips SHA verification of downloaded chunks
	SkipVerify bool
	// InfoOnly prints download info without actually downloading
	InfoOnly bool
	// Verbose shows per-file progress instead of just overall progress
	Verbose bool
}

// DefaultDownloadOptions returns download options with sensible defaults
func DefaultDownloadOptions() DownloadOptions {
	return DownloadOptions{
		MaxDownloadWorkers: DefaultMaxDownloadWorkers,
		MaxMemoryUsage:     DefaultMaxMemoryUsage,
		SkipVerify:         false,
		InfoOnly:           false,
	}
}

// DownloadRequest contains all information needed to download a game
type DownloadRequest struct {
	Product     *auth.Product
	Version     *auth.ProductVersion
	InstallPath string
	Options     DownloadOptions
}

// ChunkDownload represents a chunk to be downloaded
type ChunkDownload struct {
	FileIndex  int    // Index of the file this chunk belongs to
	ChunkIndex int    // Index of this chunk within the file (for ordering)
	ChunkSHA   string // SHA of the chunk (used for download URL and verification)
	FilePath   string // Destination file path
}

// DownloadedChunk represents a chunk that has been downloaded
type DownloadedChunk struct {
	FileIndex  int    // Index of the file this chunk belongs to
	ChunkIndex int    // Index of this chunk within the file
	Data       []byte // The downloaded chunk data
	Error      error  // Any error that occurred during download
}

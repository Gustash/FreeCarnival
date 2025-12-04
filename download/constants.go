package download

import "runtime"

const (
	// ContentURL is the base URL for downloading game content
	ContentURL = "https://content.indiegalacdn.com"

	// MaxChunkSize is the maximum size of a single chunk (1 MiB)
	MaxChunkSize = 1048576
)

var (
	// DefaultMaxDownloadWorkers is the default number of parallel download workers.
	// Uses min(NumCPU * 2, 16) like Nile/Legendary:
	// - CPU count is a proxy for overall system capability (RAM, storage speed)
	// - Cap at 16 to avoid overwhelming CDN or server rate limiting
	// - 2x multiplier because workers mostly wait on network I/O
	DefaultMaxDownloadWorkers = min(runtime.NumCPU()*2, 16)

	// DefaultMaxMemoryUsage is the default maximum memory usage for buffering chunks (1 GiB)
	DefaultMaxMemoryUsage = MaxChunkSize * 1024
)

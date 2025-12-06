package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/manifest"
	"github.com/gustash/freecarnival/progress"
)

const (
	maxRetries     = 3
	retryBaseDelay = 500 * time.Millisecond
)

// ChunkDownloader handles downloading individual chunks from the CDN.
type ChunkDownloader struct {
	client   *http.Client
	product  *auth.Product
	buildOS  auth.BuildOS
	progress *progress.Tracker
}

// NewChunkDownloader creates a new chunk downloader.
func NewChunkDownloader(client *http.Client, product *auth.Product, buildOS auth.BuildOS, pt *progress.Tracker) *ChunkDownloader {
	return &ChunkDownloader{
		client:   client,
		product:  product,
		buildOS:  buildOS,
		progress: pt,
	}
}

// Download downloads a chunk with automatic retries for transient failures.
func (cd *ChunkDownloader) Download(ctx context.Context, fileIndex int, chunkSHA string) ([]byte, error) {
	url := manifest.GetChunkURL(cd.product, cd.buildOS, chunkSHA)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		data, err := cd.doDownload(ctx, url)
		if err == nil {
			if cd.progress != nil {
				cd.progress.ChunkDownloaded(fileIndex, int64(len(data)))
			}
			return data, nil
		}

		lastErr = err

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isRetryableError(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

func (cd *ChunkDownloader) doDownload(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "galaClient")

	resp, err := cd.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode}
	}

	return io.ReadAll(resp.Body)
}

// HTTPError represents an HTTP error response.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d", e.StatusCode)
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}

	errStr := err.Error()
	if strings.Contains(errStr, "stream error") ||
		strings.Contains(errStr, "INTERNAL_ERROR") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "EOF") {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}

	return false
}

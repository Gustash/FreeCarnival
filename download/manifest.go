package download

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gustash/freecarnival/auth"
)

// FetchBuildManifest downloads and parses the build manifest for a product version
// Returns the parsed records and the raw CSV data for caching
func FetchBuildManifest(ctx context.Context, client *http.Client, product *auth.Product, version *auth.ProductVersion) ([]BuildManifestRecord, []byte, error) {
	url := fmt.Sprintf("%s/DevShowCaseSourceVolume/dev_fold_%s/%s/%s/%s_manifest.csv",
		ContentURL,
		product.Namespace,
		product.IDKeyName,
		version.OS,
		version.Version,
	)

	data, err := fetchCSV(ctx, client, url)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch build manifest: %w", err)
	}

	records, err := parseBuildManifest(data)
	if err != nil {
		return nil, nil, err
	}

	return records, data, nil
}

// FetchChunksManifest downloads and parses the chunks manifest for a product version
func FetchChunksManifest(ctx context.Context, client *http.Client, product *auth.Product, version *auth.ProductVersion) ([]BuildManifestChunksRecord, error) {
	url := fmt.Sprintf("%s/DevShowCaseSourceVolume/dev_fold_%s/%s/%s/%s_manifest_chunks.csv",
		ContentURL,
		product.Namespace,
		product.IDKeyName,
		version.OS,
		version.Version,
	)

	data, err := fetchCSV(ctx, client, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chunks manifest: %w", err)
	}

	return parseChunksManifest(data)
}

func fetchCSV(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "galaClient")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func parseBuildManifest(data []byte) ([]BuildManifestRecord, error) {
	reader := csv.NewReader(bytes.NewReader(data))

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	// Build column index map
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	var records []BuildManifestRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		record := BuildManifestRecord{}

		if idx, ok := colIndex["Size in Bytes"]; ok && idx < len(row) {
			record.SizeInBytes, _ = strconv.Atoi(row[idx])
		}
		if idx, ok := colIndex["Chunks"]; ok && idx < len(row) {
			record.Chunks, _ = strconv.Atoi(row[idx])
		}
		if idx, ok := colIndex["SHA"]; ok && idx < len(row) {
			record.SHA = row[idx]
		}
		if idx, ok := colIndex["Flags"]; ok && idx < len(row) {
			record.Flags, _ = strconv.Atoi(row[idx])
		}
		if idx, ok := colIndex["File Name"]; ok && idx < len(row) {
			// Handle Latin-1 encoding and normalize path separators
			record.FileName = normalizePath(latin1ToUTF8(row[idx]))
		}
		if idx, ok := colIndex["Change Tag"]; ok && idx < len(row) {
			record.ChangeTag = row[idx]
		}

		records = append(records, record)
	}

	return records, nil
}

func parseChunksManifest(data []byte) ([]BuildManifestChunksRecord, error) {
	reader := csv.NewReader(bytes.NewReader(data))

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	// Build column index map
	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	var records []BuildManifestChunksRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		record := BuildManifestChunksRecord{}

		if idx, ok := colIndex["ID"]; ok && idx < len(row) {
			record.ID, _ = strconv.Atoi(row[idx])
		}
		if idx, ok := colIndex["Filepath"]; ok && idx < len(row) {
			// Handle Latin-1 encoding and normalize path separators
			record.FilePath = normalizePath(latin1ToUTF8(row[idx]))
		}
		if idx, ok := colIndex["Chunk SHA"]; ok && idx < len(row) {
			record.ChunkSHA = row[idx]
		}

		records = append(records, record)
	}

	return records, nil
}

// GetChunkURL returns the download URL for a chunk
func GetChunkURL(product *auth.Product, os auth.BuildOS, chunkSHA string) string {
	return fmt.Sprintf("%s/DevShowCaseSourceVolume/dev_fold_%s/%s/%s/%s",
		ContentURL,
		product.Namespace,
		product.IDKeyName,
		os,
		chunkSHA,
	)
}

// latin1ToUTF8 converts a Latin-1 (ISO-8859-1) encoded string to UTF-8.
// The CSV manifests from IndieGala contain Latin-1 encoded filenames.
func latin1ToUTF8(s string) string {
	var buf strings.Builder
	buf.Grow(len(s) * 2) // Worst case: all bytes become 2-byte UTF-8
	for i := 0; i < len(s); i++ {
		buf.WriteRune(rune(s[i]))
	}
	return buf.String()
}

// normalizePath converts Windows-style backslashes to OS-appropriate separators
// and ensures the path is valid for the current filesystem.
func normalizePath(path string) string {
	// Convert backslashes to forward slashes first
	path = strings.ReplaceAll(path, "\\", "/")
	// Then convert to OS-appropriate separator
	return filepath.FromSlash(path)
}

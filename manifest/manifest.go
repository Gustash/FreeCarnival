// Package manifest handles parsing IndieGala build manifests.
package manifest

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

// ContentURL is the base URL for downloading game content.
const ContentURL = "https://content.indiegalacdn.com"

// MaxChunkSize is the maximum size of a single chunk (1 MiB).
const MaxChunkSize = 1048576

// BuildRecord represents a file entry in the build manifest CSV.
type BuildRecord struct {
	SizeInBytes int
	Chunks      int
	SHA         string
	Flags       int
	FileName    string
	ChangeTag   string
}

// IsDirectory returns true if this record represents a directory.
func (r *BuildRecord) IsDirectory() bool {
	return r.Flags == 40
}

// IsEmpty returns true if the file has no content.
func (r *BuildRecord) IsEmpty() bool {
	return r.SizeInBytes == 0
}

// ChunkRecord represents a chunk entry in the chunks manifest CSV.
type ChunkRecord struct {
	ID       int
	FilePath string
	ChunkSHA string
}

// FetchBuild downloads and parses the build manifest for a product version.
// Returns the parsed records and the raw CSV data for caching.
func FetchBuild(ctx context.Context, client *http.Client, product *auth.Product, version *auth.ProductVersion) ([]BuildRecord, []byte, error) {
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

// FetchChunks downloads and parses the chunks manifest for a product version.
func FetchChunks(ctx context.Context, client *http.Client, product *auth.Product, version *auth.ProductVersion) ([]ChunkRecord, error) {
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

// GetChunkURL returns the download URL for a chunk.
func GetChunkURL(product *auth.Product, os auth.BuildOS, chunkSHA string) string {
	return fmt.Sprintf("%s/DevShowCaseSourceVolume/dev_fold_%s/%s/%s/%s",
		ContentURL,
		product.Namespace,
		product.IDKeyName,
		os,
		chunkSHA,
	)
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

func parseBuildManifest(data []byte) ([]BuildRecord, error) {
	reader := csv.NewReader(bytes.NewReader(data))

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	var records []BuildRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		record := BuildRecord{}

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
			record.FileName = normalizePath(latin1ToUTF8(row[idx]))
		}
		if idx, ok := colIndex["Change Tag"]; ok && idx < len(row) {
			record.ChangeTag = row[idx]
		}

		records = append(records, record)
	}

	return records, nil
}

func parseChunksManifest(data []byte) ([]ChunkRecord, error) {
	reader := csv.NewReader(bytes.NewReader(data))

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	var records []ChunkRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		record := ChunkRecord{}

		if idx, ok := colIndex["ID"]; ok && idx < len(row) {
			record.ID, _ = strconv.Atoi(row[idx])
		}
		if idx, ok := colIndex["Filepath"]; ok && idx < len(row) {
			record.FilePath = normalizePath(latin1ToUTF8(row[idx]))
		}
		if idx, ok := colIndex["Chunk SHA"]; ok && idx < len(row) {
			record.ChunkSHA = row[idx]
		}

		records = append(records, record)
	}

	return records, nil
}

// latin1ToUTF8 converts a Latin-1 (ISO-8859-1) encoded string to UTF-8.
func latin1ToUTF8(s string) string {
	var buf strings.Builder
	buf.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		buf.WriteRune(rune(s[i]))
	}
	return buf.String()
}

// normalizePath converts Windows-style backslashes to OS-appropriate separators.
func normalizePath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	return filepath.FromSlash(path)
}

// ExtractSHA extracts the actual SHA256 hash from a chunk identifier.
// Chunk identifiers are in the format: {prefix}_{index}_{sha256}
func ExtractSHA(chunkID string) string {
	lastUnderscore := strings.LastIndex(chunkID, "_")
	if lastUnderscore == -1 {
		return chunkID
	}
	return chunkID[lastUnderscore+1:]
}


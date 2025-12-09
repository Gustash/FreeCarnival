// Package verify handles file integrity verification.
package verify

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/manifest"
)

// Result contains the result of a file verification.
type Result struct {
	FilePath string
	Expected string
	Actual   string
	Valid    bool
	Error    error
}

// Options configures the verification process.
type Options struct {
	Verbose    bool
	MaxWorkers int
}

// Installation verifies all files in an installation against the manifest.
func Installation(slug string, installInfo *auth.InstallInfo, opts Options) (bool, []Result, error) {
	manifestData, err := auth.LoadManifest(slug, installInfo.Version, "manifest")
	if err != nil {
		return false, nil, fmt.Errorf("failed to load manifest: %w", err)
	}

	records, err := parseManifest(manifestData)
	if err != nil {
		return false, nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	var files []manifest.BuildRecord
	for _, record := range records {
		if !record.IsDirectory() {
			files = append(files, record)
		}
	}

	if len(files) == 0 {
		return true, nil, nil
	}

	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = runtime.NumCPU()
	}

	workCh := make(chan manifest.BuildRecord, len(files))
	resultsCh := make(chan Result, len(files))

	var verified atomic.Int64
	totalFiles := int64(len(files))

	var wg sync.WaitGroup
	for i := 0; i < opts.MaxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for record := range workCh {
				result := verifyFile(installInfo.InstallPath, record)
				resultsCh <- result

				count := verified.Add(1)
				if opts.Verbose {
					status := "OK"
					if !result.Valid {
						status = "FAILED"
					}
					fmt.Printf("[%d/%d] %s: %s\n", count, totalFiles, result.FilePath, status)
				}
			}
		}()
	}

	for _, file := range files {
		workCh <- file
	}
	close(workCh)

	wg.Wait()
	close(resultsCh)

	var results []Result
	allValid := true
	for result := range resultsCh {
		results = append(results, result)
		if !result.Valid {
			allValid = false
		}
	}

	return allValid, results, nil
}

func verifyFile(installPath string, record manifest.BuildRecord) Result {
	filePath := manifest.NormalizePath(filepath.Join(installPath, record.FileName))

	result := Result{
		FilePath: record.FileName,
		Expected: record.SHA,
	}

	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		result.Error = fmt.Errorf("file missing")
		result.Valid = false
		return result
	}
	if err != nil {
		result.Error = fmt.Errorf("failed to stat file: %w", err)
		result.Valid = false
		return result
	}

	if info.IsDir() {
		result.Error = fmt.Errorf("expected file but found directory")
		result.Valid = false
		return result
	}

	if int(info.Size()) != record.SizeInBytes {
		result.Error = fmt.Errorf("size mismatch: expected %d, got %d", record.SizeInBytes, info.Size())
		result.Valid = false
		return result
	}

	hash, err := HashFile(filePath)
	if err != nil {
		result.Error = fmt.Errorf("failed to hash file: %w", err)
		result.Valid = false
		return result
	}

	result.Actual = hash
	result.Valid = hash == record.SHA

	if !result.Valid {
		result.Error = fmt.Errorf("hash mismatch")
	}

	return result
}

// HashFile calculates the SHA256 hash of a file.
func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// Chunk verifies a downloaded chunk against its expected SHA.
func Chunk(data []byte, expectedSHA string) bool {
	hasher := sha256.New()
	hasher.Write(data)
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	return actualSHA == expectedSHA
}

func parseManifest(data []byte) ([]manifest.BuildRecord, error) {
	reader := csv.NewReader(bytes.NewReader(data))

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[col] = i
	}

	var records []manifest.BuildRecord
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		record := manifest.BuildRecord{}

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
			record.FileName = strings.ReplaceAll(row[idx], "\\", "/")
		}
		if idx, ok := colIndex["Change Tag"]; ok && idx < len(row) {
			record.ChangeTag = row[idx]
		}

		records = append(records, record)
	}

	return records, nil
}

package download

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/gustash/freecarnival/auth"
)

// VerifyResult contains the result of a file verification
type VerifyResult struct {
	FilePath string
	Expected string
	Actual   string
	Valid    bool
	Error    error
}

// VerifyOptions configures the verification process
type VerifyOptions struct {
	// Verbose prints progress for each file
	Verbose bool
	// MaxWorkers is the number of parallel verification workers (default: NumCPU)
	MaxWorkers int
}

// VerifyInstallation verifies all files in an installation against the manifest
func VerifyInstallation(slug string, installInfo *auth.InstallInfo, opts VerifyOptions) (bool, []VerifyResult, error) {
	// Load the saved manifest
	manifestData, err := auth.LoadManifest(slug, installInfo.Version, "manifest")
	if err != nil {
		return false, nil, fmt.Errorf("failed to load manifest: %w", err)
	}

	// Parse the manifest
	records, err := parseBuildManifest(manifestData)
	if err != nil {
		return false, nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Filter to only files (not directories)
	var files []BuildManifestRecord
	for _, record := range records {
		if !record.IsDirectory() {
			files = append(files, record)
		}
	}

	if len(files) == 0 {
		return true, nil, nil // No files to verify
	}

	// Set default workers
	if opts.MaxWorkers <= 0 {
		opts.MaxWorkers = DefaultMaxDownloadWorkers
	}

	// Create work channel and results
	workCh := make(chan BuildManifestRecord, len(files))
	resultsCh := make(chan VerifyResult, len(files))

	// Track progress
	var verified atomic.Int64
	totalFiles := int64(len(files))

	// Start workers
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

	// Send work
	for _, file := range files {
		workCh <- file
	}
	close(workCh)

	// Wait for completion
	wg.Wait()
	close(resultsCh)

	// Collect results
	var results []VerifyResult
	allValid := true
	for result := range resultsCh {
		results = append(results, result)
		if !result.Valid {
			allValid = false
		}
	}

	return allValid, results, nil
}

// verifyFile verifies a single file against its expected SHA
func verifyFile(installPath string, record BuildManifestRecord) VerifyResult {
	filePath := filepath.Join(installPath, record.FileName)

	result := VerifyResult{
		FilePath: record.FileName,
		Expected: record.SHA,
	}

	// Check if file exists
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

	// Check if it's actually a file
	if info.IsDir() {
		result.Error = fmt.Errorf("expected file but found directory")
		result.Valid = false
		return result
	}

	// Check file size
	if int(info.Size()) != record.SizeInBytes {
		result.Error = fmt.Errorf("size mismatch: expected %d, got %d", record.SizeInBytes, info.Size())
		result.Valid = false
		return result
	}

	// Calculate SHA256
	hash, err := hashFile(filePath)
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

// hashFile calculates the SHA256 hash of a file
func hashFile(path string) (string, error) {
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

// VerifyChunk verifies a downloaded chunk against its expected SHA
func VerifyChunk(data []byte, expectedSHA string) bool {
	hasher := sha256.New()
	hasher.Write(data)
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	return actualSHA == expectedSHA
}

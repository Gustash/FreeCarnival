// Package update handles game update operations.
package update

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gustash/freecarnival/auth"
	"github.com/gustash/freecarnival/download"
	"github.com/gustash/freecarnival/logger"
	"github.com/gustash/freecarnival/manifest"
	"github.com/gustash/freecarnival/progress"
)

// Updater manages game updates with delta manifests.
type Updater struct {
	client      *http.Client
	product     *auth.Product
	oldVersion  *auth.ProductVersion
	newVersion  *auth.ProductVersion
	installPath string
	options     download.Options
}

// New creates a new updater instance.
func New(client *http.Client, product *auth.Product, oldVersion, newVersion *auth.ProductVersion, installPath string, options download.Options) *Updater {
	return &Updater{
		client:      client,
		product:     product,
		oldVersion:  oldVersion,
		newVersion:  newVersion,
		installPath: installPath,
		options:     options,
	}
}

// Update performs the update operation.
func (u *Updater) Update(ctx context.Context) error {
	// Load old manifest
	logger.Info("Loading old manifest...")
	oldManifestData, err := auth.LoadManifest(u.product.SluggedName, u.oldVersion.Version, "manifest")
	if err != nil {
		return fmt.Errorf("failed to load old manifest: %w (try reinstalling)", err)
	}

	oldManifest, err := manifest.ParseBuildManifest(oldManifestData)
	if err != nil {
		return fmt.Errorf("failed to parse old manifest: %w", err)
	}

	// Fetch new manifest
	logger.Info("Fetching new build manifest...")
	newManifest, newManifestData, err := manifest.FetchBuild(ctx, u.client, u.product, u.newVersion)
	if err != nil {
		return fmt.Errorf("failed to fetch new build manifest: %w", err)
	}

	if err := auth.SaveManifest(u.product.SluggedName, u.newVersion.Version, "manifest", newManifestData); err != nil {
		logger.Warn("Failed to save manifest", "error", err)
	}

	// Generate delta
	logger.Info("Calculating changes...")
	delta := GenerateDelta(oldManifest, newManifest)

	if delta.IsEmpty() {
		logger.Info("No changes detected. Game is already up to date!")
		return nil
	}

	logger.Info("Update summary:")
	delta.PrintSummary()
	fmt.Println()

	if u.options.InfoOnly {
		u.printUpdateInfo(delta)
		return nil
	}

	// Fetch chunks for new/modified files
	logger.Info("Fetching chunks manifest...")
	newChunks, err := manifest.FetchChunks(ctx, u.client, u.product, u.newVersion)
	if err != nil {
		return fmt.Errorf("failed to fetch chunks manifest: %w", err)
	}

	// Filter chunks to only what we need
	deltaChunks := FilterChunksForDelta(newChunks, delta)

	// Remove modified and removed files
	logger.Debug("Cleaning up modified and removed files...")
	if err := u.cleanupFiles(delta); err != nil {
		return err
	}

	// Build combined manifest for installation
	combinedManifest := CombineManifests(delta)

	// Use downloader to install the delta
	return u.downloadDelta(ctx, combinedManifest, deltaChunks)
}

func (u *Updater) cleanupFiles(delta *DeltaManifest) error {
	// Remove modified files (they'll be re-downloaded)
	for _, record := range delta.Modified {
		if record.IsDirectory() {
			continue
		}
		filePath := filepath.Join(u.installPath, record.FileName)
		logger.Debug("Removing modified file", "file", record.FileName)
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", filePath, err)
		}
	}

	// Remove deleted files and directories
	for _, record := range delta.Removed {
		filePath := filepath.Join(u.installPath, record.FileName)
		logger.Debug("Removing deleted file", "file", record.FileName)

		if record.IsDirectory() {
			if err := os.RemoveAll(filePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove directory %s: %w", filePath, err)
			}
		} else {
			if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove %s: %w", filePath, err)
			}
		}
	}

	return nil
}

func (u *Updater) downloadDelta(ctx context.Context, deltaManifest []manifest.BuildRecord, deltaChunks []manifest.ChunkRecord) error {
	// Calculate totals
	var totalBytes int64
	var totalFiles int
	for _, record := range deltaManifest {
		if !record.IsDirectory() && !record.IsEmpty() && record.ChangeTag != string(ChangeTagRemoved) {
			totalBytes += int64(record.SizeInBytes)
			totalFiles++
		}
	}

	if totalFiles == 0 {
		logger.Info("No files to download")
		return nil
	}

	logger.Info("Starting download", "size", progress.FormatBytes(totalBytes), "files", totalFiles)

	// Prepare installation (create directories, empty files)
	fileInfoMap, err := u.prepareInstallation(deltaManifest)
	if err != nil {
		return fmt.Errorf("failed to prepare installation: %w", err)
	}

	// Group chunks by file
	fileChunks := u.groupChunksByFile(deltaChunks, fileInfoMap)

	// Create a temporary downloader for the delta
	downloader := download.New(u.client, u.product, u.newVersion, u.options)

	// Use the downloader's internal methods (we'll need to expose these or duplicate)
	return downloader.DownloadDelta(ctx, fileInfoMap, fileChunks, totalBytes, totalFiles)
}

func (u *Updater) prepareInstallation(records []manifest.BuildRecord) (map[string]*download.FileInfo, error) {
	fileInfoMap := make(map[string]*download.FileInfo)
	fileIndex := 0

	for _, record := range records {
		// Skip removed files
		if record.ChangeTag == string(ChangeTagRemoved) {
			continue
		}

		fullPath := filepath.Join(u.installPath, record.FileName)

		if record.IsDirectory() {
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", fullPath, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create parent directory for %s: %w", fullPath, err)
		}

		if record.IsEmpty() {
			f, err := os.Create(fullPath)
			if err != nil {
				return nil, fmt.Errorf("failed to create empty file %s: %w", fullPath, err)
			}
			f.Close()
			continue
		}

		fileInfoMap[record.FileName] = &download.FileInfo{
			Index:      fileIndex,
			Record:     record,
			FullPath:   fullPath,
			ChunkCount: record.Chunks,
		}
		fileIndex++
	}

	return fileInfoMap, nil
}

func (u *Updater) groupChunksByFile(chunks []manifest.ChunkRecord, fileInfoMap map[string]*download.FileInfo) map[int][]manifest.ChunkRecord {
	fileChunks := make(map[int][]manifest.ChunkRecord)

	for _, chunk := range chunks {
		info, ok := fileInfoMap[chunk.FilePath]
		if !ok {
			continue
		}
		fileChunks[info.Index] = append(fileChunks[info.Index], chunk)
	}

	return fileChunks
}

func (u *Updater) printUpdateInfo(delta *DeltaManifest) {
	var downloadSize int64
	var newDiskSize int64
	var oldDiskSize int64

	// Calculate download size (added + modified)
	for _, record := range delta.Added {
		downloadSize += int64(record.SizeInBytes)
		newDiskSize += int64(record.SizeInBytes)
	}
	for _, record := range delta.Modified {
		downloadSize += int64(record.SizeInBytes)
		newDiskSize += int64(record.SizeInBytes)
	}

	// Calculate removed size
	for _, record := range delta.Removed {
		oldDiskSize += int64(record.SizeInBytes)
	}

	netDiskChange := newDiskSize - oldDiskSize

	fmt.Println("\n=== Update Info ===")
	fmt.Printf("Download Size: %s\n", progress.FormatBytes(downloadSize))
	if netDiskChange >= 0 {
		fmt.Printf("Disk Space Required: +%s\n", progress.FormatBytes(netDiskChange))
	} else {
		fmt.Printf("Disk Space Freed: %s\n", progress.FormatBytes(-netDiskChange))
	}
}

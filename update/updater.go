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
	var oldManifest []manifest.BuildRecord
	oldManifestData, err := auth.LoadManifest(u.product.SluggedName, u.oldVersion.Version, "manifest")
	if err != nil {
		logger.Debug("Couldn't find local manifest. Fetching from server.")
		oldManifest, oldManifestData, err = manifest.FetchBuild(ctx, u.client, u.product, u.oldVersion)

		if err != nil {
			return fmt.Errorf("failed to load old manifest: %w (try reinstalling)", err)
		}
	} else {
		oldManifest, err = manifest.ParseBuildManifest(oldManifestData)

		if err != nil {
			return fmt.Errorf("failed to parse old manifest: %w", err)
		}
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

	// Build combined manifest for installation
	combinedManifest := CombineManifests(delta)

	// Check if this is a resume by looking for files from the new version
	isResume := u.checkForResumeUpdate(delta, oldManifest)

	// Remove modified and removed files (but only if not resuming)
	if !isResume {
		logger.Debug("Cleaning up modified and removed files...")
		if err := u.cleanupFiles(delta); err != nil {
			return err
		}
	} else {
		logger.Debug("Skipping cleanup (resuming existing download)")
		// Only clean up removed files
		if err := u.cleanupRemovedFiles(delta); err != nil {
			return err
		}
	}

	// Use downloader to install the delta
	return u.downloadDelta(ctx, combinedManifest, deltaChunks)
}

func (u *Updater) checkForResumeUpdate(delta *DeltaManifest, oldManifest []manifest.BuildRecord) bool {
	// Build map of old files for quick lookup
	oldFiles := make(map[string]manifest.BuildRecord)
	for _, record := range oldManifest {
		oldFiles[record.FileName] = record
	}

	// Check if any "Added" files exist (they shouldn't in a fresh update)
	for _, record := range delta.Added {
		if record.IsDirectory() || record.IsEmpty() {
			continue
		}
		filePath := filepath.Join(u.installPath, record.FileName)
		if _, err := os.Stat(filePath); err == nil {
			// Added file exists - must be resuming
			return true
		}
	}

	// Check if any "Modified" files have different size than old version
	// (indicating they were deleted and partially re-downloaded with new version)
	for _, record := range delta.Modified {
		if record.IsDirectory() || record.IsEmpty() {
			continue
		}
		filePath := filepath.Join(u.installPath, record.FileName)
		stat, err := os.Stat(filePath)
		if err != nil {
			// File doesn't exist, that's expected if cleanup already ran
			continue
		}

		oldRecord, exists := oldFiles[record.FileName]
		if !exists {
			// Shouldn't happen, but if it does, treat as resume
			return true
		}

		// If size doesn't match old version, it must be from new version download
		if stat.Size() != int64(oldRecord.SizeInBytes) {
			return true
		}
	}

	return false
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

	return u.cleanupRemovedFiles(delta)
}

func (u *Updater) cleanupRemovedFiles(delta *DeltaManifest) error {
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
		if !record.IsDirectory() && !record.IsEmpty() && record.ChangeTag != manifest.ChangeTagRemoved {
			totalBytes += int64(record.SizeInBytes)
			totalFiles++
		}
	}

	if totalFiles == 0 {
		logger.Info("No files to download")
		return nil
	}

	logger.Info("Starting download", "size", progress.FormatBytes(totalBytes), "files", totalFiles)

	downloader := download.New(u.client, u.product, u.newVersion, u.options)
	return downloader.Download(ctx, u.installPath, deltaManifest, deltaChunks)
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

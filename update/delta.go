// Package update handles game update operations with delta manifests.
package update

import (
	"fmt"

	"github.com/gustash/freecarnival/manifest"
)

// ChangeTag represents the type of change for a file.
type ChangeTag string

const (
	// ChangeTagAdded indicates a file was added.
	ChangeTagAdded ChangeTag = "added"
	// ChangeTagModified indicates a file was modified.
	ChangeTagModified ChangeTag = "modified"
	// ChangeTagRemoved indicates a file was removed.
	ChangeTagRemoved ChangeTag = "removed"
)

// DeltaManifest represents the changes between two versions.
type DeltaManifest struct {
	Added    []manifest.BuildRecord
	Modified []manifest.BuildRecord
	Removed  []manifest.BuildRecord
}

// GenerateDelta compares old and new manifests and returns the delta.
func GenerateDelta(oldManifest, newManifest []manifest.BuildRecord) *DeltaManifest {
	delta := &DeltaManifest{
		Added:    []manifest.BuildRecord{},
		Modified: []manifest.BuildRecord{},
		Removed:  []manifest.BuildRecord{},
	}

	// Build maps for quick lookup
	oldFiles := make(map[string]manifest.BuildRecord)
	newFiles := make(map[string]manifest.BuildRecord)

	for _, record := range oldManifest {
		oldFiles[record.FileName] = record
	}
	for _, record := range newManifest {
		newFiles[record.FileName] = record
	}

	// Find added and modified files
	for fileName, newRecord := range newFiles {
		oldRecord, exists := oldFiles[fileName]
		if !exists {
			// File was added
			newRecord.ChangeTag = string(ChangeTagAdded)
			delta.Added = append(delta.Added, newRecord)
		} else if oldRecord.SHA != newRecord.SHA {
			// File was modified (SHA changed)
			newRecord.ChangeTag = string(ChangeTagModified)
			delta.Modified = append(delta.Modified, newRecord)
		}
	}

	// Find removed files
	for fileName, oldRecord := range oldFiles {
		if _, exists := newFiles[fileName]; !exists {
			oldRecord.ChangeTag = string(ChangeTagRemoved)
			delta.Removed = append(delta.Removed, oldRecord)
		}
	}

	return delta
}

// CombineManifests creates a manifest for update operations.
// It includes added/modified files and marks removed files.
func CombineManifests(delta *DeltaManifest) []manifest.BuildRecord {
	var combined []manifest.BuildRecord

	// Add all added files
	combined = append(combined, delta.Added...)

	// Add all modified files
	combined = append(combined, delta.Modified...)

	// Add all removed files
	combined = append(combined, delta.Removed...)

	return combined
}

// FilterChunksForDelta filters chunk records to only include chunks for files in the delta.
func FilterChunksForDelta(allChunks []manifest.ChunkRecord, delta *DeltaManifest) []manifest.ChunkRecord {
	// Build set of files that need chunks (added/modified only, not removed)
	needChunks := make(map[string]bool)

	for _, record := range delta.Added {
		if !record.IsDirectory() && !record.IsEmpty() {
			needChunks[record.FileName] = true
		}
	}
	for _, record := range delta.Modified {
		if !record.IsDirectory() && !record.IsEmpty() {
			needChunks[record.FileName] = true
		}
	}

	// Filter chunks
	var filtered []manifest.ChunkRecord
	for _, chunk := range allChunks {
		if needChunks[chunk.FilePath] {
			filtered = append(filtered, chunk)
		}
	}

	return filtered
}

// PrintSummary prints a summary of the delta changes.
func (d *DeltaManifest) PrintSummary() {
	if len(d.Added) > 0 {
		fmt.Printf("  Added: %d files\n", len(d.Added))
	}
	if len(d.Modified) > 0 {
		fmt.Printf("  Modified: %d files\n", len(d.Modified))
	}
	if len(d.Removed) > 0 {
		fmt.Printf("  Removed: %d files\n", len(d.Removed))
	}
}

// IsEmpty returns true if there are no changes.
func (d *DeltaManifest) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Modified) == 0 && len(d.Removed) == 0
}


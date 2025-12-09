package download

import (
	"context"
	"fmt"
	"os"

	"github.com/gustash/freecarnival/manifest"
	"github.com/gustash/freecarnival/progress"
)

// FileWriteState tracks the state of writing a single file.
type FileWriteState struct {
	Info          *FileInfo
	File          *os.File
	PendingChunks map[int][]byte
	NextChunkIdx  int
	TotalChunks   int
}

// DiskWriter handles writing downloaded chunks to disk in the correct order.
type DiskWriter struct {
	memory   *MemoryLimiter
	progress *progress.Tracker
}

// NewDiskWriter creates a new disk writer.
func NewDiskWriter(memory *MemoryLimiter, pt *progress.Tracker) *DiskWriter {
	return &DiskWriter{
		memory:   memory,
		progress: pt,
	}
}

// WriteChunks reads from the chunks channel and writes them to disk in order.
func (dw *DiskWriter) WriteChunks(ctx context.Context, chunks <-chan ChunkResult, fileInfoMap map[int]*FileInfo, fileChunkCounts map[int]int, resumeState *ResumeState) error {
	fileStates := make(map[int]*FileWriteState)

	defer func() {
		for _, state := range fileStates {
			if state.File != nil {
				state.File.Close()
			}
		}
	}()

	for chunk := range chunks {
		if err := dw.processChunk(chunk, fileStates, fileInfoMap, fileChunkCounts, resumeState); err != nil {
			return err
		}
	}

	if ctx.Err() != nil {
		return nil
	}

	for fileIdx, state := range fileStates {
		if err := dw.flushPendingChunks(state); err != nil {
			return err
		}

		if state.NextChunkIdx < state.TotalChunks {
			return fmt.Errorf("file %s incomplete: got %d/%d chunks", state.Info.FullPath, state.NextChunkIdx, state.TotalChunks)
		}

		if state.File != nil {
			state.File.Close()
			state.File = nil
		}
		if dw.progress != nil {
			dw.progress.FileComplete(fileIdx)
		}
	}

	return nil
}

func (dw *DiskWriter) processChunk(chunk ChunkResult, fileStates map[int]*FileWriteState, fileInfoMap map[int]*FileInfo, fileChunkCounts map[int]int, resumeState *ResumeState) error {
	if chunk.Error != nil {
		info := fileInfoMap[chunk.FileIndex]
		if info != nil {
			return fmt.Errorf("download error for %s: %w", info.FullPath, chunk.Error)
		}
		return fmt.Errorf("download error for file %d: %w", chunk.FileIndex, chunk.Error)
	}

	state, exists := fileStates[chunk.FileIndex]
	if !exists {
		var err error
		state, err = dw.createFileState(chunk.FileIndex, fileInfoMap, fileChunkCounts, resumeState)
		if err != nil {
			dw.memory.Release(manifest.MaxChunkSize)
			return err
		}
		if state == nil {
			dw.memory.Release(manifest.MaxChunkSize)
			return nil
		}
		fileStates[chunk.FileIndex] = state
	}

	if chunk.ChunkIndex == state.NextChunkIdx {
		if err := dw.writeChunkData(state, chunk.Data); err != nil {
			return err
		}

		if err := dw.flushPendingChunks(state); err != nil {
			return err
		}

		if state.NextChunkIdx >= state.TotalChunks {
			state.File.Close()
			state.File = nil

			if dw.progress != nil {
				dw.progress.FileComplete(state.Info.Index)
			}

			delete(fileStates, chunk.FileIndex)
		}
	} else {
		state.PendingChunks[chunk.ChunkIndex] = chunk.Data
	}

	return nil
}

func (dw *DiskWriter) createFileState(fileIndex int, fileInfoMap map[int]*FileInfo, fileChunkCounts map[int]int, resumeState *ResumeState) (*FileWriteState, error) {
	info := fileInfoMap[fileIndex]
	if info == nil {
		return nil, nil
	}

	appendMode := false
	startChunkIdx := 0
	if resumeState != nil {
		startChunkIdx = resumeState.StartChunkIndex[fileIndex]
		appendMode = startChunkIdx > 0 && !resumeState.CorruptedFiles[fileIndex]
	}

	var f *os.File
	var err error
	if appendMode {
		f, err = os.OpenFile(info.FullPath, os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			f, err = os.Create(info.FullPath)
		}
	} else {
		f, err = os.Create(info.FullPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", info.FullPath, err)
	}

	return &FileWriteState{
		Info:          info,
		File:          f,
		PendingChunks: make(map[int][]byte),
		NextChunkIdx:  0,
		TotalChunks:   fileChunkCounts[fileIndex],
	}, nil
}

func (dw *DiskWriter) writeChunkData(state *FileWriteState, data []byte) error {
	if _, err := state.File.Write(data); err != nil {
		dw.memory.Release(manifest.MaxChunkSize)
		return fmt.Errorf("failed to write to %s: %w", state.Info.FullPath, err)
	}
	chunkSize := int64(len(data))
	dw.memory.Release(manifest.MaxChunkSize)

	if dw.progress != nil {
		dw.progress.ChunkWritten(state.Info.Index, chunkSize)
	}
	state.NextChunkIdx++
	return nil
}

func (dw *DiskWriter) flushPendingChunks(state *FileWriteState) error {
	for {
		pendingData, exists := state.PendingChunks[state.NextChunkIdx]
		if !exists {
			break
		}
		if err := dw.writeChunkData(state, pendingData); err != nil {
			return err
		}
		delete(state.PendingChunks, state.NextChunkIdx-1)
	}
	return nil
}

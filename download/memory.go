package download

import (
	"context"
	"sync"
	"sync/atomic"
)

// MemoryLimiter manages memory usage for concurrent operations.
// It ensures the total memory used doesn't exceed a configured maximum.
type MemoryLimiter struct {
	maxMemory  int64
	memoryUsed atomic.Int64
	memoryMu   sync.Mutex
	memoryCond *sync.Cond
}

// NewMemoryLimiter creates a new memory limiter with the given maximum memory usage.
func NewMemoryLimiter(maxMemory int64) *MemoryLimiter {
	m := &MemoryLimiter{
		maxMemory: maxMemory,
	}
	m.memoryCond = sync.NewCond(&m.memoryMu)
	return m
}

// Acquire blocks until the requested memory is available, then reserves it.
// Returns immediately if the context is cancelled.
func (m *MemoryLimiter) Acquire(ctx context.Context, size int64) bool {
	m.memoryMu.Lock()
	defer m.memoryMu.Unlock()

	for m.memoryUsed.Load()+size > m.maxMemory {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		m.memoryCond.Wait()
	}
	m.memoryUsed.Add(size)
	return true
}

// Release frees the specified amount of memory.
func (m *MemoryLimiter) Release(size int64) {
	m.memoryUsed.Add(-size)
	m.memoryCond.Broadcast()
}

// Used returns the current memory usage.
func (m *MemoryLimiter) Used() int64 {
	return m.memoryUsed.Load()
}


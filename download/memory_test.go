package download

import (
	"context"
	"testing"
)

func TestMemoryLimiter_AcquireRelease(t *testing.T) {
	memory := NewMemoryLimiter(1024)

	ctx := context.Background()

	if !memory.Acquire(ctx, 512) {
		t.Error("Acquire should succeed")
	}

	if memory.Used() != 512 {
		t.Errorf("memoryUsed = %d, expected 512", memory.Used())
	}

	memory.Release(512)
	if memory.Used() != 0 {
		t.Errorf("memoryUsed after release = %d, expected 0", memory.Used())
	}
}

func TestMemoryLimiter_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	memory := NewMemoryLimiter(100)

	if memory.Acquire(ctx, 1024) {
		t.Error("Acquire should return false for cancelled context")
	}
}

package common

import (
	"context"
	"sync"
)

// WaitWorkers bounds the caller's wait; cancellation is the worker owner's responsibility.
func WaitWorkers(ctx context.Context, workers *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

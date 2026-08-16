package db

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPuzzleCacheRaceCondition(t *testing.T) {
	pc := newPuzzleCache(30 * time.Minute)
	ctx := context.Background()
	key := uint64(12345)
	maxCount := uint32(10)

	var wg sync.WaitGroup
	numGoroutines := 100
	numIterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				pc.Inc(ctx, key, 30*time.Minute)
			}
		}()

		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				pc.CheckCount(ctx, key, maxCount)
			}
		}()
	}

	wg.Wait()
}

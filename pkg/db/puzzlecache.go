package db

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/maypok86/otter/v2"
)

type puzzleCache struct {
	store *otter.Cache[uint64, *uint32]
}

func newPuzzleCache(expiryTTL time.Duration) *puzzleCache {
	const maxSize = 500_000
	const initialSize = 1_000

	return &puzzleCache{
		store: otter.Must(&otter.Options[uint64, *uint32]{
			MaximumSize:      maxSize,
			InitialCapacity:  initialSize,
			ExpiryCalculator: otter.ExpiryCreating[uint64, *uint32](expiryTTL),
			Logger:           &pcOtterLogger{},
		}),
	}
}

func (pc *puzzleCache) CheckCount(ctx context.Context, key uint64, maxCount uint32) bool {
	if count, ok := pc.store.GetIfPresent(key); ok {
		slog.Log(ctx, common.LevelTrace, "Puzzle is found in cache", "key", key, "count", count)
		return *count < maxCount
	}

	return true
}

func puzzleCacheMap() (newValue *uint32, cancel bool) {
	return new(uint32), false
}

func (pc *puzzleCache) Inc(ctx context.Context, key uint64, ttl time.Duration) uint32 {
	value, _ := pc.store.ComputeIfAbsent(key, puzzleCacheMap)
	result := atomic.AddUint32(value, 1)

	if result == 1 {
		pc.store.SetExpiresAfter(key, ttl)
	}

	return result
}

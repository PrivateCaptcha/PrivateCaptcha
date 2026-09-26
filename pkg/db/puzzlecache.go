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

type PuzzleReservation struct {
	count *uint32
}

func (r *PuzzleReservation) Release() {
	atomic.AddUint32(r.count, ^uint32(0))
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

func puzzleCacheMap() (newValue *uint32, cancel bool) {
	return new(uint32), false
}

func (pc *puzzleCache) Reserve(ctx context.Context, key uint64, maxCount uint32, ttl time.Duration) (*PuzzleReservation, bool) {
	value, _ := pc.store.ComputeIfAbsent(key, puzzleCacheMap)
	for {
		count := atomic.LoadUint32(value)
		if count >= maxCount {
			return nil, false
		}
		if atomic.CompareAndSwapUint32(value, count, count+1) {
			if count == 0 {
				pc.store.SetExpiresAfter(key, ttl)
			}
			slog.Log(ctx, common.LevelTrace, "Reserved puzzle verification", "key", key, "count", count+1)
			return &PuzzleReservation{count: value}, true
		}
	}
}

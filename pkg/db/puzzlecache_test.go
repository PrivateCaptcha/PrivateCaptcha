package db

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPuzzleCacheRaceCondition(t *testing.T) {
	pc := newPuzzleCache(time.Hour)
	ctx := context.Background()
	const key uint64 = 12345
	const limit uint32 = 2
	const workers = 32
	start := make(chan struct{})
	results := make(chan *PuzzleReservation, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			reservation, ok := pc.Reserve(ctx, key, limit, time.Hour)
			if ok {
				results <- reservation
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var reservations []*PuzzleReservation
	for reservation := range results {
		reservations = append(reservations, reservation)
	}
	if len(reservations) != int(limit) {
		t.Fatalf("reserved %d slots, want %d", len(reservations), limit)
	}
	if _, ok := pc.Reserve(ctx, key, limit, time.Hour); ok {
		t.Fatal("full puzzle cache should reject another verification")
	}
	reservations[0].Release()
	if _, ok := pc.Reserve(ctx, key, limit, time.Hour); !ok {
		t.Fatal("released reservation was not available for retry")
	}
}

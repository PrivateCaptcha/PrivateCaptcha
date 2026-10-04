package leakybucket

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestRaceSaveCacheVsAdd drives concurrent Add+Update writers against concurrent
// Manager.SaveCache readers over the same *Manager. It deliberately does not call
// Manager.Level, so the only read side exercised is the production SaveCache path
// (Hottest() -> snapshot/Level filter -> GobEncode). Without snapshotting each
// bucket under cache.Compute, this is a Go data race flagged by `go test -race`.
// Regression check for the SaveCache read-side race fixed by snapshotting under
// Compute (mirrors the writer-side fix in PR #392 / commit e8cde012).
func TestRaceSaveCacheVsAdd(t *testing.T) {
	const maxBuckets = 1024
	const cap = 50
	manager := NewManager[int32, ConstLeakyBucket[int32]](maxBuckets, cap, 100*time.Millisecond)
	tnow := time.Now().Truncate(1 * time.Second)

	for k := int32(1); k <= 100; k++ {
		manager.Add(k, 10, tnow)
	}

	stop := make(chan struct{})
	var stopWg sync.WaitGroup

	for w := 0; w < 4; w++ {
		stopWg.Add(1)
		go func(seed int32) {
			defer stopWg.Done()
			i := seed
			for {
				select {
				case <-stop:
					return
				default:
				}
				manager.Add(((i % 100) + 1), 1, tnow.Add(time.Duration(i)*time.Millisecond))
				i++
			}
		}(int32(w * 1000))
	}

	for w := 0; w < 2; w++ {
		stopWg.Add(1)
		go func(seed int32) {
			defer stopWg.Done()
			i := seed
			for {
				select {
				case <-stop:
					return
				default:
				}
				manager.Update(((i % 100) + 1), cap, 100*time.Millisecond, tnow.Add(time.Duration(i)*time.Millisecond))
				i++
			}
		}(int32(w * 1000))
	}

	for r := 0; r < 32; r++ {
		stopWg.Add(1)
		go func() {
			defer stopWg.Done()
			dir := t.TempDir()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = manager.SaveCache(context.Background(), dir, "race_x", 100, tnow)
			}
		}()
	}

	time.Sleep(2000 * time.Millisecond)
	close(stop)
	stopWg.Wait()
}

// TestRaceSaveCacheVsAddEx seeds a VarLeakyBucket manager and drives AddEx writers
// against concurrent SaveCache readers. VarLeakyBucket.GobEncode reads additional
// mutable fields (leakRate/pendingSum/count) off the cached pointer; the snapshot
// must detach those too.
func TestRaceSaveCacheVsAddEx(t *testing.T) {
	const maxBuckets = 1024
	const cap = 50
	manager := NewManager[int32, VarLeakyBucket[int32]](maxBuckets, cap, 100*time.Millisecond)
	tnow := time.Now().Truncate(1 * time.Second)

	for k := int32(1); k <= 100; k++ {
		manager.AddEx(k, 10, tnow, cap, 100*time.Millisecond)
	}

	stop := make(chan struct{})
	var stopWg sync.WaitGroup

	for w := 0; w < 4; w++ {
		stopWg.Add(1)
		go func(seed int32) {
			defer stopWg.Done()
			i := seed
			for {
				select {
				case <-stop:
					return
				default:
				}
				manager.AddEx(((i % 100) + 1), 1, tnow.Add(time.Duration(i)*time.Millisecond), cap, 100*time.Millisecond)
				i++
			}
		}(int32(w * 1000))
	}

	for w := 0; w < 2; w++ {
		stopWg.Add(1)
		go func(seed int32) {
			defer stopWg.Done()
			i := seed
			for {
				select {
				case <-stop:
					return
				default:
				}
				manager.Update(((i % 100) + 1), cap, 100*time.Millisecond, tnow.Add(time.Duration(i)*time.Millisecond))
				i++
			}
		}(int32(w * 1000))
	}

	for r := 0; r < 32; r++ {
		stopWg.Add(1)
		go func() {
			defer stopWg.Done()
			dir := t.TempDir()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = manager.SaveCache(context.Background(), dir, "race_var", 100, tnow)
			}
		}()
	}

	time.Sleep(2000 * time.Millisecond)
	close(stop)
	stopWg.Wait()
}

package difficulty

import (
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type timeStats struct {
	m sync.Mutex
	// map from unix timestamp to count
	counts map[int64]uint32
	max    int64
}

func newTimeStats() *timeStats {
	return &timeStats{
		counts: make(map[int64]uint32),
		max:    0,
	}
}

func (s *timeStats) stats(keys ...int64) ([]uint32, bool) {
	s.m.Lock()
	defer s.m.Unlock()

	result := make([]uint32, 0, len(keys))
	anyFound := false

	for _, key := range keys {
		if count, ok := s.counts[key]; ok {
			result = append(result, count)
			anyFound = true
		} else {
			result = append(result, 0)
		}
	}

	return result, anyFound
}

func (s *timeStats) inc(key int64, value uint32) uint32 {
	s.m.Lock()
	defer s.m.Unlock()

	var result uint32

	if count, ok := s.counts[key]; ok {
		s.counts[key] = count + value
		result = count + value
	} else {
		s.counts[key] = value
		result = 1
		if key > s.max {
			s.max = key
		}
	}

	return result
}

func (s *timeStats) backfill(bucketSize time.Duration, counts []*common.TimeCount) {
	s.m.Lock()
	defer s.m.Unlock()

	for _, c := range counts {
		timeKey := c.Timestamp.Truncate(bucketSize).Unix()
		if count, ok := s.counts[timeKey]; ok && count > c.Count {
			continue
		}

		s.counts[timeKey] = c.Count
	}
}

// cleanup deletes entries that are earlier than {before} and returns if the map is empty after cleanup
func (s *timeStats) cleanup(before int64) bool {
	s.m.Lock()
	defer s.m.Unlock()

	//if s.min > before {
	//	return false
	//}

	if (0 < s.max) && (s.max < before) {
		s.counts = make(map[int64]uint32)
		s.max = 0
		return true
	}

	size := len(s.counts)
	deleted := 0
	leftMax := int64(0)

	for key := range s.counts {
		if key < before {
			delete(s.counts, key)
			deleted++
		} else {
			if key > leftMax {
				leftMax = key
			}
		}
	}

	s.max = leftMax
	return (size == deleted)
}

type Counts struct {
	// property stats map from internal ID (int32) to stats
	stats      map[int32]*timeStats
	lock       sync.RWMutex
	bucketSize time.Duration
}

func newCounts(bucketSize time.Duration) *Counts {
	return &Counts{
		stats:      make(map[int32]*timeStats),
		bucketSize: bucketSize,
	}
}

func (c *Counts) fetchStats(key int32) *timeStats {
	var st *timeStats
	var ok bool

	{
		c.lock.RLock()
		st, ok = c.stats[key]
		c.lock.RUnlock()
	}

	if !ok {
		c.lock.Lock()
		{
			if prev, ok := c.stats[key]; !ok {
				st = newTimeStats()
				c.stats[key] = st
			} else {
				st = prev
			}
		}
		c.lock.Unlock()
	}

	return st
}

func (c *Counts) Inc(pid int32, fingerprint common.TFingerprint, t time.Time) uint32 {
	st := c.fetchStats(pid)
	timeKey := t.Truncate(c.bucketSize).Unix()
	return st.inc(timeKey, 1)
}

func (c *Counts) BackfillProperty(pid int32, counts []*common.TimeCount) {
	st := c.fetchStats(pid)
	st.backfill(c.bucketSize, counts)
}

func (c *Counts) Clear() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.stats = make(map[int32]*timeStats)
}

type Stats struct {
	// last 5 buckets of stats before time {t} (first element is the furthest in time)
	Property    []uint32
	HasProperty bool
}

func (st *Stats) Sum(decayRate float64) float64 {
	sum := 0.0

	for _, c := range st.Property {
		sum = sum*(1.0-decayRate) + float64(c)
	}

	return sum
}

func (c *Counts) FetchStats(pid int32, fingerprint common.TFingerprint, t time.Time) Stats {
	var ps *timeStats
	var ok bool
	{
		c.lock.RLock()
		ps, ok = c.stats[pid]
		c.lock.RUnlock()
	}

	var stats Stats

	if !ok {
		return stats
	}

	tunix := t.Truncate(c.bucketSize).Unix()
	bucketSize := int64(c.bucketSize.Seconds())

	stats.Property, stats.HasProperty = ps.stats(
		tunix-4*bucketSize,
		tunix-3*bucketSize,
		tunix-2*bucketSize,
		tunix-1*bucketSize,
		tunix)

	return stats
}

// Cleanup removes timeStats entries that precede last {buckets} before time {t} and
// returns how many properties were removed
func (c *Counts) Cleanup(t time.Time, buckets int, maxToDelete int) int {
	if maxToDelete == 0 {
		return 0
	}

	before := t.Add(-time.Duration(buckets) * c.bucketSize).Unix()

	toDelete := make([]int32, 0)
	{
		c.lock.RLock()
		for key, value := range c.stats {
			if empty := value.cleanup(before); empty {
				toDelete = append(toDelete, key)
				if len(toDelete) >= maxToDelete {
					break
				}
			}
		}
		c.lock.RUnlock()
	}

	deleted := 0

	if len(toDelete) > 0 {
		// NOTE: it's possible that in the interval between these 2 loops some items will get incremented (again)
		// however, values should be so small that it is considered fine to lose them
		c.lock.Lock()
		for _, key := range toDelete {
			delete(c.stats, key)
			deleted++
		}
		c.lock.Unlock()
	}

	return deleted
}

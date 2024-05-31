package difficulty

import (
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type timeStats struct {
	m sync.Mutex
	// property ID (aka key in hashmap)
	pid int32
	// map from unix timestamp to count
	counts map[int64]uint32
	// earliest time in .counts (unix timestamp only grows)
	max int64
	// supporting structure for LRU linked list
	prev *timeStats
	next *timeStats
}

func newTimeStats(pid int32) *timeStats {
	return &timeStats{
		pid:    pid,
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

func (s *timeStats) inc(key int64, value uint32) (uint32, int) {
	s.m.Lock()
	defer s.m.Unlock()

	var result uint32

	if count, ok := s.counts[key]; ok {
		s.counts[key] = count + value
		result = count + value
	} else {
		s.counts[key] = value
		result = value

		if key > s.max {
			s.max = key
		}
	}

	return result, len(s.counts)
}

func (s *timeStats) backfill(bucketSize time.Duration, counts []*common.TimeCount) int {
	s.m.Lock()
	defer s.m.Unlock()

	for _, c := range counts {
		timeKey := c.Timestamp.Truncate(bucketSize).Unix()
		if count, ok := s.counts[timeKey]; ok && count > c.Count {
			continue
		}

		s.counts[timeKey] = c.Count

		if timeKey > s.max {
			s.max = timeKey
		}
	}

	return len(s.counts)
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
	lock       sync.Mutex
	bucketSize time.Duration
	buckets    int
	head       *timeStats
	tail       *timeStats
	// if we overflow upperBound, we cleanup down to lowerBound
	upperBound int
	lowerBound int
}

func newCounts(bucketSize time.Duration, buckets int, cap int) *Counts {
	return &Counts{
		stats:      make(map[int32]*timeStats),
		bucketSize: bucketSize,
		buckets:    buckets,
		upperBound: cap,
		lowerBound: cap/2 + cap/4,
	}
}

func (c *Counts) get(key int32) (*timeStats, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	st, ok := c.stats[key]
	if ok {
		c.removeUnsafe(st)
		c.addUnsafe(st)
	}

	return st, ok
}

func (c *Counts) fetchStats(key int32) *timeStats {
	c.lock.Lock()
	defer c.lock.Unlock()

	st, ok := c.stats[key]
	if ok {
		c.removeUnsafe(st)
		c.addUnsafe(st)
		return st
	}

	st = newTimeStats(key)
	c.stats[key] = st
	c.addUnsafe(st)

	if (c.upperBound > 0) && (len(c.stats) > c.upperBound) {
		delete(c.stats, c.tail.pid)
		c.removeUnsafe(c.tail)
	}

	return st
}

func (c *Counts) addUnsafe(node *timeStats) {
	node.prev = nil
	node.next = c.head

	if c.head != nil {
		c.head.prev = node
	}

	c.head = node

	if c.tail == nil {
		c.tail = node
	}
}

func (c *Counts) removeUnsafe(node *timeStats) {
	if node != c.head {
		node.prev.next = node.next
	} else {
		c.head = node.next
	}

	if node != c.tail {
		node.next.prev = node.prev
	} else {
		c.tail = node.prev
	}
}

func (c *Counts) Inc(pid int32, fingerprint common.TFingerprint, t time.Time) uint32 {
	st := c.fetchStats(pid)
	timeKey := t.Truncate(c.bucketSize).Unix()

	result, buckets := st.inc(timeKey, 1)

	// we can afford a small cleanup as Inc() is only called in the background batch processing
	if buckets > c.buckets {
		before := t.Add(-time.Duration(c.buckets) * c.bucketSize).Unix()
		st.cleanup(before)
	}

	return result
}

func (c *Counts) BackfillProperty(pid int32, counts []*common.TimeCount, t time.Time) {
	st := c.fetchStats(pid)
	buckets := st.backfill(c.bucketSize, counts)

	if buckets > c.buckets {
		before := t.Add(-time.Duration(c.buckets) * c.bucketSize).Unix()
		st.cleanup(before)
	}
}

func (c *Counts) Clear() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.stats = make(map[int32]*timeStats)
	c.head = nil
	c.tail = nil
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
	var stats Stats

	ps, ok := c.get(pid)
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

func (c *Counts) compressUnsafe(cap int) int {
	if cap <= 0 {
		return 0
	}

	deleted := 0

	for len(c.stats) > cap {
		delete(c.stats, c.tail.pid)
		c.removeUnsafe(c.tail)
		deleted++
	}

	return deleted
}

// Cleanup removes timeStats entries that precede last {buckets} before time {t} and
// returns how many properties were removed
func (c *Counts) CleanupEx(t time.Time, buckets int, maxToCleanup int) int {
	if maxToCleanup == 0 {
		return 0
	}

	before := t.Add(-time.Duration(buckets) * c.bucketSize).Unix()
	toDelete := make([]*timeStats, 0)

	// we do full locking here, but we execute up to maxToCleanup operations
	c.lock.Lock()
	defer c.lock.Unlock()

	compressCap := len(c.stats) - maxToCleanup
	if compressCap < c.lowerBound {
		compressCap = c.lowerBound
	}

	deleted := c.compressUnsafe(compressCap)
	if deleted >= maxToCleanup {
		return deleted
	}

	cleanedUp := deleted
	for node := c.tail; node != nil; node = node.prev {
		if empty := node.cleanup(before); empty {
			toDelete = append(toDelete, node)
		}

		cleanedUp++
		if cleanedUp >= maxToCleanup {
			break
		}
	}

	// we added elements from the end of the LRU list so need to reverse the iteration
	for i := len(toDelete) - 1; i >= 0; i-- {
		node := toDelete[i]
		delete(c.stats, node.pid)
		c.removeUnsafe(node)
		deleted++
	}

	return deleted
}

func (c *Counts) Cleanup(t time.Time, maxToCleanup int) int {
	return c.CleanupEx(t, c.buckets, maxToCleanup)
}

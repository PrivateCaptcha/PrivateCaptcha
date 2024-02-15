package difficulty

import (
	"sync"
	"sync/atomic"
	"time"
)

type accessRecord struct {
	Fingerprint string
	UserID      int32
	PropertyID  int32
	Timestamp   time.Time
}

type TimeCount struct {
	Timestamp time.Time
	Count     uint32
}

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

func (s *timeStats) backfill(bucketSize time.Duration, counts []*TimeCount) {
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

type propertyStats struct {
	perTime *timeStats
	perUser *userStats
}

type Counts struct {
	// property stats map from internal ID (int32) to stats
	stats      map[int32]propertyStats
	lock       sync.RWMutex
	bucketSize time.Duration
}

func newCounts(bucketSize time.Duration) *Counts {
	return &Counts{
		stats:      make(map[int32]propertyStats),
		bucketSize: bucketSize,
	}
}

func (c *Counts) fetchStats(key int32) propertyStats {
	var st propertyStats
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
				st = propertyStats{
					perTime: newTimeStats(),
					perUser: newUserStats(),
				}
				c.stats[key] = st
			} else {
				st = prev
			}
		}
		c.lock.Unlock()
	}

	return st
}

func (c *Counts) Inc(pid int32, fingerprint string, t time.Time) uint32 {
	st := c.fetchStats(pid)
	st.perUser.Inc(fingerprint, t, c.bucketSize)
	timeKey := t.Truncate(c.bucketSize).Unix()
	return st.perTime.inc(timeKey, 1)
}

func (c *Counts) BackfillProperty(pid int32, counts []*TimeCount) {
	st := c.fetchStats(pid)
	st.perTime.backfill(c.bucketSize, counts)
}

func (c *Counts) BackfillUser(pid int32, fingerprint string, counts []*TimeCount) {
	st := c.fetchStats(pid)
	st.perUser.Backfill(fingerprint, counts, c.bucketSize)
}

func (c *Counts) Clear() {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.stats = make(map[int32]propertyStats)
}

type Stats struct {
	// last 5 buckets of stats before time {t} (first element is the furthest in time)
	Property    []uint32
	User        uint32
	HasProperty bool
	HasUser     bool
}

func (st *Stats) Sum(decayRate float64) float64 {
	sum := 0.0

	for _, c := range st.Property {
		sum = sum*(1.0-decayRate) + float64(c)
	}

	if (len(st.Property) > 0) && (st.Property[len(st.Property)-1] > st.User) && (st.User > 0) {
		// don't count userCounts twice since it's already included in the last time bucket of this property
		sum -= float64(st.User)
	}

	if st.User > 0 {
		sum = sum*(1.0-decayRate) + float64(st.User)
	}

	return sum
}

func (c *Counts) FetchStats(pid int32, fingerprint string, t time.Time) Stats {
	var ps propertyStats
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

	stats.Property, stats.HasProperty = ps.perTime.stats(
		tunix-4*bucketSize,
		tunix-3*bucketSize,
		tunix-2*bucketSize,
		tunix-1*bucketSize,
		tunix)

	stats.User, stats.HasUser = ps.perUser.Count(fingerprint, t, c.bucketSize)

	return stats
}

// Cleanup removes timeStats entries that precede last {buckets} before time {t} and
// returns how many properties were removed
func (c *Counts) Cleanup(t time.Time, buckets int, maxToDelete int) int {
	if maxToDelete == 0 {
		return 0
	}

	before := t.Add(-time.Duration(buckets) * c.bucketSize).Unix()
	usersDeleted := 0

	toDelete := make([]int32, 0)
	{
		c.lock.RLock()
		for key, value := range c.stats {
			if empty := value.perTime.cleanup(before); empty {
				toDelete = append(toDelete, key)
				if len(toDelete) >= maxToDelete {
					break
				}
			} else if cleanedUp := value.perUser.Cleanup(t, c.bucketSize, maxToDelete); cleanedUp == maxToDelete {
				usersDeleted++
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

	return deleted + usersDeleted
}

type singleStats struct {
	time  atomic.Int64
	count atomic.Uint32
}

func (ss *singleStats) backfill(t int64, count uint32) uint32 {
	oldTime := ss.time.Load()
	if oldTime > t {
		return ss.count.Load()
	}

	swapped := false

	for oldTime < t && !swapped {
		swapped = ss.time.CompareAndSwap(oldTime, t)
		if swapped {
			ss.count.Store(0)
		} else {
			oldTime = ss.time.Load()
		}
	}

	oldCount := ss.count.Load()
	for oldCount < count && !ss.count.CompareAndSwap(oldCount, count) {
		oldCount = ss.count.Load()
	}

	return ss.count.Load()
}

func (ss *singleStats) inc(t int64, count uint32) uint32 {
	// the goal is to make the following code "safer":
	// if (time == t) { count++ } else { time = t; count = 1 }

	oldTime := ss.time.Load()
	if oldTime > t {
		return ss.count.Load()
	}

	swapped := false

	for oldTime < t && !swapped {
		swapped = ss.time.CompareAndSwap(oldTime, t)
		if swapped {
			ss.count.Store(0)
		} else {
			oldTime = ss.time.Load()
		}
	}

	return ss.count.Add(count)
}

type userStats struct {
	// user stats map from fingerprint to stats
	counts map[string]*singleStats
	lock   sync.RWMutex
}

func newUserStats() *userStats {
	return &userStats{
		counts: make(map[string]*singleStats),
	}
}

func (us *userStats) fetchStats(key string) *singleStats {
	var st *singleStats
	var ok bool

	{
		us.lock.RLock()
		st, ok = us.counts[key]
		us.lock.RUnlock()
	}

	if !ok {
		us.lock.Lock()
		{
			if prev, ok := us.counts[key]; !ok {
				st = &singleStats{}
				us.counts[key] = st
			} else {
				st = prev
			}
		}
		us.lock.Unlock()
	}

	return st
}

func (us *userStats) Inc(key string, t time.Time, bucketSize time.Duration) uint32 {
	timeKey := t.Truncate(bucketSize).Unix()
	st := us.fetchStats(key)
	return st.inc(timeKey, 1)
}

func (us *userStats) Backfill(key string, counts []*TimeCount, bucketSize time.Duration) {
	st := us.fetchStats(key)
	for _, c := range counts {
		timeKey := c.Timestamp.Truncate(bucketSize).Unix()
		st.backfill(timeKey, c.Count)
	}
}

func (us *userStats) Clear() {
	us.lock.Lock()
	defer us.lock.Unlock()
	us.counts = make(map[string]*singleStats)
}

func (us *userStats) Count(key string, t time.Time, bucketSize time.Duration) (uint32, bool) {
	if len(key) == 0 {
		return 0, false
	}

	timeKey := t.Truncate(bucketSize).Unix()

	us.lock.RLock()
	defer us.lock.RUnlock()

	st, ok := us.counts[key]
	if ok && st.time.Load() == timeKey {
		return st.count.Load(), true
	}

	return 0, ok
}

// Cleanup() removes {maxToDelete} entries before time {t}
func (us *userStats) Cleanup(t time.Time, bucketSize time.Duration, maxToDelete int) int {
	if maxToDelete == 0 {
		return 0
	}

	before := t.Add(-bucketSize).Unix()

	toDelete := make([]string, 0)
	{
		us.lock.RLock()
		for key, value := range us.counts {
			if value.time.Load() < before {
				toDelete = append(toDelete, key)
				if len(toDelete) >= maxToDelete {
					break
				}
			}
		}
		us.lock.RUnlock()
	}

	deleted := 0

	if len(toDelete) > 0 {
		// NOTE: it's possible that in the interval between these 2 loops some items will get incremented (again)
		// however, values should be so small that it is considered fine to lose them
		us.lock.Lock()
		for _, key := range toDelete {
			delete(us.counts, key)
			deleted++
		}
		us.lock.Unlock()
	}

	return deleted
}

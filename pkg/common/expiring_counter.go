package common

import (
	"time"

	"github.com/puzpuzpuz/xsync/v4"
)

type counter struct {
	counter  uint32
	lastSeen time.Time
}

// ExpiringCounterMap is a concurrent per-key counter with last-seen timestamps.
//
// The zero value is ready to use. Do not copy an ExpiringCounterMap after first use.
type ExpiringCounterMap[Key comparable] struct {
	m *xsync.Map[Key, counter]
}

func NewExpiringCounterMap[Key comparable]() *ExpiringCounterMap[Key] {
	return &ExpiringCounterMap[Key]{
		m: xsync.NewMap[Key, counter](),
	}
}

// Get returns the current counter value for key.
// It returns 0 if key does not exist.
func (c *ExpiringCounterMap[Key]) Get(key Key) (uint32, bool) {
	item, ok := c.m.Load(key)
	if !ok {
		return 0, false
	}

	return item.counter, true
}

// Inc increments the counter for key and returns the previous value.
func (c *ExpiringCounterMap[Key]) Inc(key Key) uint32 {
	now := time.Now()

	var old uint32
	c.m.Compute(key, func(item counter, loaded bool) (counter, xsync.ComputeOp) {
		if loaded {
			old = item.counter
			item.counter++
			item.lastSeen = now
			return item, xsync.UpdateOp
		}

		old = 0
		return counter{
			counter:  1,
			lastSeen: now,
		}, xsync.UpdateOp
	})

	return old
}

// Delete removes key from the counter.
func (c *ExpiringCounterMap[Key]) Delete(key Key) {
	c.m.Delete(key)
}

// ClearExpired deletes up to maxItems expired entries.
//
// maxItems limits how many entries are deleted, not how many are inspected.
// It returns the number of deleted entries.
func (c *ExpiringCounterMap[Key]) ClearExpired(expiration time.Duration, maxItems int) int {
	if maxItems <= 0 {
		return 0
	}

	deleted := 0
	now := time.Now()

	c.m.Range(func(key Key, item counter) bool {
		if deleted >= maxItems {
			return false
		}

		if now.Sub(item.lastSeen) <= expiration {
			return true
		}

		// Re-check under Compute so we do not delete a value that was refreshed
		// by a concurrent Inc after Range observed an old snapshot.
		_, ok := c.m.Compute(key, func(current counter, loaded bool) (counter, xsync.ComputeOp) {
			if !loaded {
				return current, xsync.CancelOp
			}
			if now.Sub(current.lastSeen) > expiration {
				return current, xsync.DeleteOp
			}
			return current, xsync.CancelOp
		})

		if !ok {
			deleted++
		}

		return deleted < maxItems
	})

	return deleted
}

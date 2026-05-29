package common

import (
	"sync"
	"testing"
	"time"
)

func TestExpiringCounterIncReturnsOldValue(t *testing.T) {
	c := NewExpiringCounterMap[string]()

	if old := c.Inc("a"); old != 0 {
		t.Fatalf("first Inc returned %d, want 0", old)
	}
	if old := c.Inc("a"); old != 1 {
		t.Fatalf("second Inc returned %d, want 1", old)
	}
	if old := c.Inc("a"); old != 2 {
		t.Fatalf("third Inc returned %d, want 2", old)
	}

	item, ok := c.m.Load("a")
	if !ok {
		t.Fatal("counter missing")
	}
	if item.counter != 3 {
		t.Fatalf("counter = %d, want 3", item.counter)
	}
	if item.lastSeen.IsZero() {
		t.Fatal("lastSeen was not set")
	}
}

func TestExpiringCounterDelete(t *testing.T) {
	c := NewExpiringCounterMap[string]()

	c.Inc("a")
	c.Delete("a")

	if _, ok := c.m.Load("a"); ok {
		t.Fatal("counter still exists after Delete")
	}

	if old := c.Inc("a"); old != 0 {
		t.Fatalf("Inc after Delete returned %d, want 0", old)
	}
}

func TestExpiringCounterClearExpired(t *testing.T) {
	c := NewExpiringCounterMap[string]()

	now := time.Now()
	c.m.Store("expired-1", counter{counter: 1, lastSeen: now.Add(-time.Hour)})
	c.m.Store("expired-2", counter{counter: 2, lastSeen: now.Add(-time.Hour)})
	c.m.Store("fresh", counter{counter: 3, lastSeen: now})

	deleted := c.ClearExpired(time.Minute, 10)
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}

	if _, ok := c.m.Load("expired-1"); ok {
		t.Fatal("expired-1 still exists")
	}
	if _, ok := c.m.Load("expired-2"); ok {
		t.Fatal("expired-2 still exists")
	}
	if _, ok := c.m.Load("fresh"); !ok {
		t.Fatal("fresh was deleted")
	}
}

func TestExpiringCounterClearExpiredHonorsMaxItems(t *testing.T) {
	c := NewExpiringCounterMap[int]()

	old := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		c.m.Store(i, counter{counter: uint32(i), lastSeen: old})
	}

	deleted := c.ClearExpired(time.Minute, 2)
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}

	remaining := 0
	c.m.Range(func(_ int, _ counter) bool {
		remaining++
		return true
	})

	if remaining != 3 {
		t.Fatalf("remaining = %d, want 3", remaining)
	}
}

func TestExpiringCounterClearExpiredWithNonPositiveMaxItems(t *testing.T) {
	c := NewExpiringCounterMap[string]()

	c.m.Store("expired", counter{
		counter:  1,
		lastSeen: time.Now().Add(-time.Hour),
	})

	if deleted := c.ClearExpired(time.Minute, 0); deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
	if _, ok := c.m.Load("expired"); !ok {
		t.Fatal("entry was deleted despite maxItems=0")
	}
}

func TestExpiringCounterConcurrentInc(t *testing.T) {
	c := NewExpiringCounterMap[string]()

	const goroutines = 16
	const incrementsPerGoroutine = 1_000

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incrementsPerGoroutine; j++ {
				c.Inc("shared")
			}
		}()
	}

	wg.Wait()

	item, ok := c.m.Load("shared")
	if !ok {
		t.Fatal("counter missing")
	}

	want := uint32(goroutines * incrementsPerGoroutine)
	if item.counter != want {
		t.Fatalf("counter = %d, want %d", item.counter, want)
	}
}

func TestExpiringCounterGet(t *testing.T) {
	c := NewExpiringCounterMap[string]()

	if got, ok := c.Get("missing"); ok || got != 0 {
		t.Fatalf("Get missing = %d, want 0", got)
	}

	c.Inc("a")
	c.Inc("a")

	if got, ok := c.Get("a"); !ok || got != 2 {
		t.Fatalf("Get a = %d, want 2", got)
	}

	c.Delete("a")

	if got, ok := c.Get("a"); ok || got != 0 {
		t.Fatalf("Get deleted = %d, want 0", got)
	}
}

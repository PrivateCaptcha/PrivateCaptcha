package db

import (
	"context"
	"errors"
	"sync"
)

var (
	errFull = errors.New("maximum size reached")
)

type StaticCache[TKey comparable, TValue comparable] struct {
	cache        map[TKey]TValue
	mux          sync.RWMutex
	upperBound   int
	lowerBound   int
	missingValue TValue
}

func NewStaticCache[TKey comparable, TValue comparable](capacity int, missingValue TValue) *StaticCache[TKey, TValue] {
	return &StaticCache[TKey, TValue]{
		cache:        make(map[TKey]TValue),
		upperBound:   capacity,
		lowerBound:   capacity/2 + capacity/4,
		missingValue: missingValue,
	}
}

func (c *StaticCache[TKey, TValue]) Get(ctx context.Context, key TKey) (TValue, error) {
	c.mux.RLock()
	defer c.mux.RUnlock()

	if item, ok := c.cache[key]; ok {
		if item == c.missingValue {
			return c.missingValue, ErrNegativeCacheHit
		}

		return item, nil
	} else {
		return c.missingValue, ErrCacheMiss
	}
}

func (c *StaticCache[TKey, TValue]) SetMissing(ctx context.Context, key TKey) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	c.cache[key] = c.missingValue
	return nil
}

func (c *StaticCache[TKey, TValue]) compressUnsafe() {
	for k := range c.cache {
		if len(c.cache) <= c.lowerBound {
			break
		}

		delete(c.cache, k)
	}
}

func (c *StaticCache[TKey, TValue]) Set(ctx context.Context, key TKey, t TValue) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	if len(c.cache) >= c.upperBound {
		c.compressUnsafe()
	}

	c.cache[key] = t
	return nil
}

func (c *StaticCache[TKey, TValue]) Delete(ctx context.Context, key TKey) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	delete(c.cache, key)
	return nil
}

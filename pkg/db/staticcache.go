package db

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/puzpuzpuz/xsync/v4"
)

type StaticCache[TKey comparable, TValue comparable] struct {
	cache        *xsync.Map[TKey, TValue]
	compressMux  sync.Mutex
	upperBound   int
	lowerBound   int
	missingValue TValue
}

var _ common.Cache[int, any] = (*StaticCache[int, any])(nil)

func NewStaticCache[TKey comparable, TValue comparable](capacity int, missingValue TValue) *StaticCache[TKey, TValue] {
	return &StaticCache[TKey, TValue]{
		cache:        xsync.NewMap[TKey, TValue](xsync.WithPresize(capacity)),
		upperBound:   capacity,
		lowerBound:   capacity/2 + capacity/4,
		missingValue: missingValue,
	}
}

func (c *StaticCache[TKey, TValue]) HitRatio() float64 {
	// unsupported
	return 0.0
}

func (c *StaticCache[TKey, TValue]) Missing() TValue {
	return c.missingValue
}

func (c *StaticCache[TKey, TValue]) NeedsRefresh(_ TKey) bool {
	return false
}

func (c *StaticCache[TKey, TValue]) Get(ctx context.Context, key TKey) (TValue, error) {
	item, ok := c.cache.Load(key)
	if !ok {
		return c.missingValue, ErrCacheMiss
	}

	if item == c.missingValue {
		return c.missingValue, ErrNegativeCacheHit
	}

	return item, nil
}

func (c *StaticCache[TKey, TValue]) GetEx(ctx context.Context, key TKey, loader common.CacheLoader[TKey, TValue]) (TValue, error) {
	var loadErr error
	actual, ok := c.cache.Compute(key, func(oldValue TValue, loaded bool) (TValue, xsync.ComputeOp) {
		if loaded {
			return oldValue, xsync.CancelOp
		}
		val, err := loader.Load(ctx, key)
		if err != nil {
			loadErr = err
			var zero TValue
			return zero, xsync.CancelOp
		}
		return val, xsync.UpdateOp
	})

	if loadErr != nil {
		slog.ErrorContext(ctx, "Failed to load the value", "key", key, common.ErrAttr(loadErr))
		return c.missingValue, ErrCacheMiss
	}

	if !ok {
		return c.missingValue, ErrCacheMiss
	}

	if actual == c.missingValue {
		return c.missingValue, ErrNegativeCacheHit
	}

	return actual, nil
}

func (c *StaticCache[TKey, TValue]) SetMissing(ctx context.Context, key TKey) error {
	c.cache.Store(key, c.missingValue)
	return nil
}

func (c *StaticCache[TKey, TValue]) compress() {
	toDelete := c.cache.Size() - c.lowerBound
	if toDelete <= 0 {
		return
	}
	keys := make([]TKey, 0, toDelete)
	c.cache.Range(func(key TKey, _ TValue) bool {
		keys = append(keys, key)
		return len(keys) < toDelete
	})
	for _, key := range keys {
		c.cache.Delete(key)
	}
}

func (c *StaticCache[TKey, TValue]) Set(ctx context.Context, key TKey, t TValue) error {
	if c.cache.Size() >= c.upperBound {
		c.compressMux.Lock()
		defer c.compressMux.Unlock()
		if c.cache.Size() >= c.upperBound {
			c.compress()
		}
	}

	c.cache.Store(key, t)
	return nil
}

func (c *StaticCache[TKey, TValue]) SetWithTTL(ctx context.Context, key TKey, t TValue, _ time.Duration) error {
	// ttl is not supported here
	return c.Set(ctx, key, t)
}

func (c *StaticCache[TKey, TValue]) SetTTL(ctx context.Context, key TKey, _ time.Duration) error {
	// ttl is not supported here
	return ErrInvalidInput
}

func (c *StaticCache[TKey, TValue]) Delete(ctx context.Context, key TKey) bool {
	_, found := c.cache.LoadAndDelete(key)
	return found
}

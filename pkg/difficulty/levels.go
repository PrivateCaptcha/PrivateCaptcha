package difficulty

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"runtime/debug"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

var (
	errBackfillPanic = errors.New("panic during backfill")
)

const (
	defaultBackpressureTimeout = 10 * time.Millisecond
)

type Levels struct {
	timeSeries          common.TimeSeriesStore
	propertyBuckets     *leakybucket.Manager[int32, leakybucket.VarLeakyBucket[int32], *leakybucket.VarLeakyBucket[int32]]
	userBuckets         *leakybucket.Manager[common.TFingerprint, leakybucket.ConstLeakyBucket[common.TFingerprint], *leakybucket.ConstLeakyBucket[common.TFingerprint]]
	accessChan          chan *common.AccessRecord
	backfillChan        chan *common.BackfillRequest
	batchSize           int
	accessLogCancel     context.CancelFunc
	backpressureTimeout time.Duration
}

func NewLevels(timeSeries common.TimeSeriesStore, batchSize int, bucketSize time.Duration) *Levels {
	const (
		propertyBucketCap = math.MaxUint32
		// below numbers are rather arbitrary as we can support "many"
		// as for users, we want to keep only the "most active" ones as "not active enough" activity
		// does not affect difficulty much, if at all
		maxUserBuckets     = 1_000_000
		maxPropertyBuckets = 100_000
		userBucketCap      = math.MaxUint32
		// user worst case: everybody in the private network (VPN, BigCorp internal) access single resource (survey, login)
		// estimate: 12 "free" requests per minute should be "enough for everybody" (tm), after that difficulty grows
		userLeakRatePerMinute = 12
		userBucketSize        = time.Minute / userLeakRatePerMinute
	)

	levels := &Levels{
		timeSeries:          timeSeries,
		propertyBuckets:     leakybucket.NewManager[int32, leakybucket.VarLeakyBucket[int32]](maxPropertyBuckets, propertyBucketCap, bucketSize),
		userBuckets:         leakybucket.NewManager[common.TFingerprint, leakybucket.ConstLeakyBucket[common.TFingerprint]](maxUserBuckets, userBucketCap, userBucketSize),
		accessChan:          make(chan *common.AccessRecord, 10*batchSize),
		backfillChan:        make(chan *common.BackfillRequest, batchSize),
		batchSize:           batchSize,
		accessLogCancel:     func() {},
		backpressureTimeout: defaultBackpressureTimeout,
	}

	return levels
}

func requestsToDifficulty(requests float64, minDifficulty float64, level dbgen.DifficultyGrowth) uint8 {
	if (requests < 1.0) || (level == dbgen.DifficultyGrowthConstant) || (minDifficulty >= 255.0) {
		return uint8(min(minDifficulty, 255.0))
	}

	// full formula is
	// y = log2(log2(x**a)) * x**b
	// parameter "a" affects sensitivity to growth

	a := 0.3
	switch level {
	case dbgen.DifficultyGrowthSlow:
		a = 0.2
	case dbgen.DifficultyGrowthMedium:
		a = 0.3
	case dbgen.DifficultyGrowthFast:
		a = 0.5
	}

	log2A := math.Log2(a)

	m := log2A
	if requests > 1.0 {
		m += math.Log10(requests)
	}
	m = math.Max(m, 0.0)

	b := math.Log2((256.0-minDifficulty)/(5.0+log2A)) / 32.0
	fx := m * math.Pow(requests, b)
	difficulty := minDifficulty + math.Round(fx)

	if difficulty >= 255.0 {
		return 255
	}

	return uint8(difficulty)
}

func (levels *Levels) BackfillTimeout() time.Duration {
	return levels.backpressureTimeout
}

func (levels *Levels) Init(accessLogInterval, backfillInterval, backpressureTimeout time.Duration) {
	levels.backpressureTimeout = max(backpressureTimeout, defaultBackpressureTimeout)

	const (
		maxPendingBatchSize = 100_000
		levelsService       = "levels"
	)
	var accessCtx context.Context
	accessBaseCtx := context.WithValue(context.Background(), common.ServiceContextKey, levelsService)
	accessCtx, levels.accessLogCancel = context.WithCancel(
		context.WithValue(accessBaseCtx, common.TraceIDContextKey, "access_log"))
	go common.ProcessBatchArray(accessCtx, levels.accessChan, accessLogInterval, levels.batchSize, maxPendingBatchSize, levels.timeSeries.WriteAccessLogBatch)

	difficultyBaseCtx := context.WithValue(context.Background(), common.ServiceContextKey, levelsService)
	go levels.backfillDifficulty(context.WithValue(difficultyBaseCtx, common.TraceIDContextKey, "backfill_difficulty"),
		backfillInterval)
}

func (l *Levels) Shutdown() {
	slog.Debug("Shutting down levels routines")
	l.accessLogCancel()
	close(l.accessChan)
	close(l.backfillChan)
}

func (l *Levels) DifficultyEx(ctx context.Context, fingerprint common.TFingerprint, p Property, baseDifficulty uint8, tnow time.Time) (uint8, leakybucket.TLevel, error) {
	err := l.recordAccess(ctx, fingerprint, p, tnow)

	minDifficulty := float64(max(p.Level(), int16(baseDifficulty)))

	propertyAddResult := l.propertyBuckets.Add(p.ID(), 1, tnow)
	if !propertyAddResult.Found {
		if perr := l.backfillProperty(ctx, p); perr != nil {
			// yes, we override, because it's not that important
			err = perr
		}
	}

	userAddResult := l.userBuckets.Add(fingerprint, 1, tnow)

	level := int64(userAddResult.CurrLevel)
	level += int64(propertyAddResult.CurrLevel)

	// just as bucket's level is the measure of deviation of requests
	// difficulty is the scaled deviation from minDifficulty
	return requestsToDifficulty(float64(level), minDifficulty, p.Growth()), propertyAddResult.CurrLevel, err
}

func (l *Levels) Difficulty(ctx context.Context, fingerprint common.TFingerprint, p Property, tnow time.Time) uint8 {
	diff, _, _ := l.DifficultyEx(ctx, fingerprint, p, 0, tnow)
	return diff
}

func (l *Levels) backfillProperty(ctx context.Context, p Property) error {
	br := &common.BackfillRequest{
		OrgID:      p.OrgID(),
		UserID:     p.OwnerID(),
		PropertyID: p.ID(),
	}

	select {
	case l.backfillChan <- br:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(l.backpressureTimeout):
		return common.ErrBackpressure
	}
}

func (l *Levels) BackfillAccess(ctx context.Context, result *puzzle.VerifyResult) error {
	ar := &common.AccessRecord{
		Fingerprint: 0, // we lose information about user but having totals still helps for difficulty calculation
		UserID:      result.UserID,
		OrgID:       result.OrgID,
		PropertyID:  result.PropertyID,
		Timestamp:   result.CreatedAt,
	}

	timer := time.NewTimer(l.backpressureTimeout)
	defer timer.Stop()

	select {
	case l.accessChan <- ar:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return common.ErrBackpressure
	}
}

func (l *Levels) recordAccess(ctx context.Context, fingerprint common.TFingerprint, p Property, tnow time.Time) error {
	if (p == nil) || !p.Valid() {
		return nil
	}

	ar := &common.AccessRecord{
		Fingerprint: fingerprint,
		UserID:      p.OwnerID(),
		OrgID:       p.OrgID(),
		PropertyID:  p.ID(),
		Timestamp:   tnow,
	}

	timer := time.NewTimer(l.backpressureTimeout)
	defer timer.Stop()

	select {
	case l.accessChan <- ar:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return common.ErrBackpressure
	}
}

func (l *Levels) Reset() {
	l.propertyBuckets.Clear()
	l.userBuckets.Clear()
}

func (l *Levels) retrievePropertyStatsSafe(ctx context.Context, r *common.BackfillRequest) (data []*common.TimeCount, err error) {
	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "Recovered from ClickHouse query panic", "panic", rvr, "stack", string(debug.Stack()))
			data = []*common.TimeCount{}
			err = errBackfillPanic
		}
	}()

	// 12 because we keep last hour of 5-minute intervals in Clickhouse, so we grab all of them
	timeFrom := time.Now().UTC().Add(-time.Duration(12) * l.propertyBuckets.LeakInterval())
	return l.timeSeries.RetrievePropertyStatsSince(ctx, r, timeFrom)
}

func (l *Levels) backfillDifficulty(ctx context.Context, cacheDuration time.Duration) {
	slog.DebugContext(ctx, "Backfilling difficulty", "cacheDuration", cacheDuration)

	const maxCacheSize = 250
	cache := make(map[string]time.Time, maxCacheSize/3)
	lastCleanupTime := time.Now()

	for r := range l.backfillChan {
		blog := slog.With("pid", r.PropertyID)
		cacheKey := r.Key()
		tnow := time.Now()
		if t, ok := cache[cacheKey]; ok && tnow.Sub(t) <= cacheDuration {
			blog.WarnContext(ctx, "Skipping duplicate backfill request", "time", t)
			continue
		}

		counts, err := l.retrievePropertyStatsSafe(ctx, r)
		if err != nil {
			blog.ErrorContext(ctx, "Failed to backfill stats", common.ErrAttr(err))
			continue
		}

		cache[cacheKey] = tnow

		if len(counts) > 0 {
			var addResult leakybucket.AddResult
			for _, count := range counts {
				addResult = l.propertyBuckets.Add(r.PropertyID, count.Count, count.Timestamp)
			}
			blog.InfoContext(ctx, "Backfilled requests counts", "counts", len(counts), "level", addResult.CurrLevel)
		}

		if (len(cache) > maxCacheSize) || (time.Since(lastCleanupTime) >= cacheDuration) {
			slog.DebugContext(ctx, "Cleaning up backfill cache", "size", len(cache))
			for key, value := range cache {
				if tnow.Sub(value) > cacheDuration {
					delete(cache, key)
				}
			}

			lastCleanupTime = time.Now()
		}
	}

	slog.DebugContext(ctx, "Finished backfilling difficulty")
}

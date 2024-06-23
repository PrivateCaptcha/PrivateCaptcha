package difficulty

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
	"github.com/jpillora/backoff"
)

const (
	LevelSmall     = 125
	LevelMedium    = 150
	LevelHigh      = 160
	bucketsCount   = 5
	leakyBucketCap = math.MaxUint32
	// this one is arbitrary as we can support "many"
	maxBucketsToKeep = 1_000_000
)

type Levels struct {
	timeSeries      *db.TimeSeriesStore
	buckets         *leakybucket.Manager[int32, leakybucket.VarLeakyBucket[int32], *leakybucket.VarLeakyBucket[int32]]
	accessChan      chan *common.AccessRecord
	backfillChan    chan *common.BackfillRequest
	batchSize       int
	accessLogCancel context.CancelFunc
	cleanupCancel   context.CancelFunc
	bucketSize      time.Duration
}

func NewLevelsEx(timeSeries *db.TimeSeriesStore, batchSize int, bucketSize, accessLogInterval, backfillInterval time.Duration) *Levels {
	levels := &Levels{
		timeSeries:   timeSeries,
		buckets:      leakybucket.NewManager[int32, leakybucket.VarLeakyBucket[int32], *leakybucket.VarLeakyBucket[int32]](maxBucketsToKeep, leakyBucketCap, 0.0 /*leakRatePerSecond*/),
		accessChan:   make(chan *common.AccessRecord, 3*batchSize/2),
		backfillChan: make(chan *common.BackfillRequest, batchSize),
		batchSize:    batchSize,
		bucketSize:   bucketSize,
	}

	var accessCtx context.Context
	accessCtx, levels.accessLogCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "access_log"))
	go levels.processAccessLog(accessCtx, accessLogInterval)

	go levels.backfillDifficulty(context.WithValue(context.Background(), common.TraceIDContextKey, "backfill_difficulty"),
		backfillInterval)

	var cancelCtx context.Context
	cancelCtx, levels.cleanupCancel = context.WithCancel(
		context.WithValue(context.Background(), common.TraceIDContextKey, "cleanup_stats"))
	go levels.cleanupStats(cancelCtx)

	return levels
}

func NewLevels(timeSeries *db.TimeSeriesStore, batchSize int, bucketSize time.Duration) *Levels {
	return NewLevelsEx(timeSeries, batchSize, bucketSize, 2*time.Second, bucketSize)
}

func minDifficultyForLevel(level dbgen.DifficultyLevel) uint8 {
	switch level {
	case dbgen.DifficultyLevelSmall:
		return LevelSmall
	case dbgen.DifficultyLevelMedium:
		return LevelMedium
	case dbgen.DifficultyLevelHigh:
		return LevelHigh
	default:
		return LevelMedium
	}
}

func requestsToDifficulty(requests float64, minDifficulty uint8, level dbgen.DifficultyGrowth) uint8 {
	if requests < 1.0 {
		return minDifficulty
	}

	// full formula is
	// y = log2(log2(x**a)) * x**b
	// parameter "a" affects sensitivity to growth

	a := 1.0
	switch level {
	case dbgen.DifficultyGrowthSlow:
		a = 0.9
	case dbgen.DifficultyGrowthMedium:
		a = 1.0
	case dbgen.DifficultyGrowthFast:
		a = 1.1
	}

	log2A := math.Log2(a)

	m := log2A
	if requests > 2.0 {
		m += math.Log2(math.Log2(requests))
	}
	m = math.Max(m, 0.0)

	b := math.Log2((256.0-float64(minDifficulty))/(5.0+log2A)) / 32.0
	fx := m * math.Pow(requests, b)
	difficulty := float64(minDifficulty) + math.Round(fx)

	if difficulty >= 255.0 {
		return 255
	}

	return uint8(difficulty)
}

func (l *Levels) Shutdown() {
	slog.Debug("Shutting down levels routines")
	close(l.accessChan)
	l.accessLogCancel()
	close(l.backfillChan)
	l.cleanupCancel()
}

func (l *Levels) DifficultyEx(fingerprint common.TFingerprint, p *dbgen.Property, tnow time.Time) (uint8, leakybucket.TLevel) {
	l.recordAccess(fingerprint, p, tnow)

	minDifficulty := minDifficultyForLevel(p.Level)

	level, added, ok := l.buckets.Add(p.ID, 1, tnow)
	if !ok {
		l.backfillProperty(p)
	}

	// just as bucket's level is the measure of deviation of requests
	// difficulty is the scaled deviation from minDifficulty
	return requestsToDifficulty(float64(level+added), minDifficulty, p.Growth), (level + added)
}

func (l *Levels) Difficulty(fingerprint common.TFingerprint, p *dbgen.Property, tnow time.Time) uint8 {
	diff, _ := l.DifficultyEx(fingerprint, p, tnow)
	return diff
}

func (l *Levels) backfillProperty(p *dbgen.Property) {
	br := &common.BackfillRequest{
		OrgID:      p.OrgID.Int32,
		UserID:     p.OrgOwnerID.Int32,
		PropertyID: p.ID,
	}
	l.backfillChan <- br
}

func (l *Levels) recordAccess(fingerprint common.TFingerprint, p *dbgen.Property, tnow time.Time) {
	if (p == nil) || !p.ExternalID.Valid {
		return
	}

	ar := &common.AccessRecord{
		Fingerprint: fingerprint,
		// we record events for the user that owns the org where the property belongs
		// (effectively, who is billed for the org), rather than who created it
		UserID:     p.OrgOwnerID.Int32,
		OrgID:      p.OrgID.Int32,
		PropertyID: p.ID,
		Timestamp:  tnow,
	}

	l.accessChan <- ar
}

func cleanupStatsImpl(ctx context.Context, maxInterval time.Duration, defaultChunkSize int, deleter func(t time.Time, size int) int) {
	b := &backoff.Backoff{
		Min:    1 * time.Second,
		Max:    maxInterval,
		Factor: 2,
		Jitter: true,
	}

	slog.DebugContext(ctx, "Starting cleaning up stats", "maxInterval", maxInterval)

	deleteChunk := defaultChunkSize

	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false
		case <-time.After(b.Duration()):
			deleted := deleter(time.Now(), deleteChunk)
			if deleted == 0 {
				deleteChunk = defaultChunkSize
				continue
			}

			slog.Debug("Deleted expired property counts", "count", deleted)

			// in case of any deletes, we want to go back to small interval first
			b.Reset()

			if deleted == deleteChunk {
				// 1.5 scaling factor
				deleteChunk += deleteChunk / 2
			}
		}
	}

	slog.DebugContext(ctx, "Finished cleaning up stats")
}

func (l *Levels) cleanupStats(ctx context.Context) {
	// bucket window is currently 5 minutes so it may change at a minute step
	// here we have 30 seconds since 1 minute sounds like way too long
	cleanupStatsImpl(ctx, 30*time.Second, 100 /*chunkSize*/, func(t time.Time, size int) int {
		return l.buckets.Cleanup(t, size)
	})
}

func (l *Levels) Reset() {
	l.buckets.Clear()
}

func (l *Levels) backfillDifficulty(ctx context.Context, cacheDuration time.Duration) {
	slog.DebugContext(ctx, "Backfilling difficulty", "cacheDuration", cacheDuration)

	cache := make(map[string]time.Time)
	const maxCacheSize = 250
	lastCleanupTime := time.Now()

	for r := range l.backfillChan {
		blog := slog.With("pid", r.PropertyID)
		cacheKey := r.Key()
		tnow := time.Now()
		if t, ok := cache[cacheKey]; ok && tnow.Sub(t) <= cacheDuration {
			blog.WarnContext(ctx, "Skipping duplicate backfill request", "time", t)
			continue
		}

		counts, err := l.timeSeries.ReadPropertyStats(ctx, r, l.bucketSize)

		if err != nil {
			blog.ErrorContext(ctx, "Failed to backfill stats", common.ErrAttr(err))
			continue
		}

		cache[cacheKey] = tnow

		if len(counts) > 0 {
			var level leakybucket.TLevel
			for _, count := range counts {
				level, _, _ = l.buckets.Add(r.PropertyID, count.Count, count.Timestamp)
			}
			blog.InfoContext(ctx, "Backfilled requests counts", "counts", len(counts), "level", level)
		}

		if (len(cache) > maxCacheSize) || (time.Since(lastCleanupTime) >= cacheDuration) {
			slog.DebugContext(ctx, "Cleaning up backfill cache")
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

func (l *Levels) processAccessLog(ctx context.Context, delay time.Duration) {
	var batch []*common.AccessRecord
	slog.DebugContext(ctx, "Processing access log", "interval", delay)

	for running := true; running; {
		select {
		case <-ctx.Done():
			running = false

		case ar, ok := <-l.accessChan:
			if !ok {
				running = false
				break
			}

			batch = append(batch, ar)

			if len(batch) >= l.batchSize {
				if err := l.timeSeries.WriteAccessLogBatch(ctx, batch); err == nil {
					slog.DebugContext(ctx, "Inserted batch of access records", "size", len(batch))
					batch = []*common.AccessRecord{}
				} else {
					slog.ErrorContext(ctx, "Failed to process batch", common.ErrAttr(err))
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				if err := l.timeSeries.WriteAccessLogBatch(ctx, batch); err == nil {
					slog.DebugContext(ctx, "Inserted batch of access records after delay", "size", len(batch))
					batch = []*common.AccessRecord{}
				} else {
					slog.ErrorContext(ctx, "Failed to process batch", common.ErrAttr(err))
				}
			}
		}
	}

	slog.InfoContext(ctx, "Finished processing access log")
}

package difficulty

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jpillora/backoff"
)

const (
	LevelSmall   = 125
	LevelMedium  = 150
	LevelHigh    = 160
	maxCacheSize = 1_000_000
	bucketsCount = 5
)

type Levels struct {
	timeSeries      *db.TimeSeriesStore
	counts          *Counts
	accessChan      chan *common.AccessRecord
	backfillChan    chan *common.BackfillRequest
	batchSize       int
	accessLogCancel context.CancelFunc
	cleanupCancel   context.CancelFunc
}

func NewLevelsEx(timeSeries *db.TimeSeriesStore, batchSize int, bucketSize, accessLogInterval, backfillInterval time.Duration) *Levels {
	levels := &Levels{
		timeSeries:   timeSeries,
		counts:       newCounts(bucketSize, bucketsCount, maxCacheSize),
		accessChan:   make(chan *common.AccessRecord, 3*batchSize/2),
		backfillChan: make(chan *common.BackfillRequest, batchSize),
		batchSize:    batchSize,
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

func decayRateForLevel(level dbgen.DifficultyGrowth) float64 {
	switch level {
	case dbgen.DifficultyGrowthSlow:
		return 0.39
	case dbgen.DifficultyGrowthMedium:
		return 0.53
	case dbgen.DifficultyGrowthFast:
		return 0.65
	default:
		return 0.5
	}
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

	// default equation
	// y = a*x^b
	// b = log2(256/a) / 32 (to fit into max difficulty 256 and 2^32 requests)
	// for details of these parameters, use gnuplot
	// we assume we will not receive more than 2^20 == 1'048'576 requests during measuring window
	// so we replace 32 -> 20 and also 256 is reduced by min difficulty to fit only the difference
	a := 1.0
	slope := 20.0
	switch level {
	case dbgen.DifficultyGrowthSlow:
		a = 1.0
	case dbgen.DifficultyGrowthMedium:
		a = 1.8
	case dbgen.DifficultyGrowthFast:
		a = 3.0
	}

	b := math.Log2((256.0-float64(minDifficulty))/a) / slope
	fx := a * math.Pow(requests, b)
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

func (l *Levels) DifficultyEx(fingerprint common.TFingerprint, p *dbgen.Property, tnow time.Time) (uint8, Stats) {
	l.recordAccess(fingerprint, p, tnow)

	stats := l.counts.FetchStats(p.ID, fingerprint, tnow)
	if !stats.HasProperty {
		l.backfillProperty(p)
	}

	decayRate := decayRateForLevel(p.Growth)
	sum := stats.Sum(decayRate)
	minDifficulty := minDifficultyForLevel(p.Level)

	return requestsToDifficulty(sum, minDifficulty, p.Growth), stats
}

func (l *Levels) Difficulty(fingerprint common.TFingerprint, p *dbgen.Property) uint8 {
	tnow := time.Now().UTC()
	d, _ := l.DifficultyEx(fingerprint, p, tnow)
	return d
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
			deleted := deleter(time.Now().UTC(), deleteChunk)
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
		return l.counts.Cleanup(t, size)
	})
}

func (l *Levels) Reset() {
	l.counts.Clear()
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

		counts, err := l.timeSeries.ReadPropertyStats(ctx, r, l.counts.bucketSize)

		if err != nil {
			blog.ErrorContext(ctx, "Failed to backfill stats", common.ErrAttr(err))
			continue
		}

		cache[cacheKey] = tnow

		if len(counts) > 0 {
			blog.DebugContext(ctx, "Backfilling requests counts", "counts", len(counts))
			l.counts.BackfillProperty(r.PropertyID, counts, tnow)
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

			l.counts.Inc(ar.PropertyID, ar.Fingerprint, ar.Timestamp)

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

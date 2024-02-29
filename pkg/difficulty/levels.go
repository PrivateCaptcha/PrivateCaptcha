package difficulty

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/jpillora/backoff"
)

const (
	LevelSmall  = 125
	LevelMedium = 150
	LevelHigh   = 160
)

const (
	tableName   = "privatecaptcha.request_logs"
	tableName5m = "privatecaptcha.request_logs_5m"
)

type backfillRequest struct {
	UserID      int32
	PropertyID  int32
	Fingerprint string
}

func (br *backfillRequest) Key() string {
	if len(br.Fingerprint) > 0 {
		return fmt.Sprintf("%d/%s", br.PropertyID, br.Fingerprint)
	}

	return strconv.Itoa(int(br.PropertyID))
}

type Levels struct {
	clickhouse      *sql.DB
	counts          *Counts
	accessChan      chan *accessRecord
	backfillChan    chan *backfillRequest
	batchSize       int
	tableName       string
	accessLogCancel context.CancelFunc
	cleanupCancel   context.CancelFunc
}

func NewLevelsEx(cf *sql.DB, batchSize int, bucketSize, accessLogInterval, backfillInterval time.Duration) *Levels {
	levels := &Levels{
		clickhouse:   cf,
		counts:       newCounts(bucketSize),
		accessChan:   make(chan *accessRecord, 3*batchSize/2),
		backfillChan: make(chan *backfillRequest, batchSize),
		batchSize:    batchSize,
		tableName:    tableName,
	}

	var accessCtx context.Context
	accessCtx, levels.accessLogCancel = context.WithCancel(context.Background())
	go levels.processAccessLog(accessCtx, accessLogInterval)

	go levels.backfillDifficulty(context.Background(), backfillInterval)

	var cancelCtx context.Context
	cancelCtx, levels.cleanupCancel = context.WithCancel(context.Background())
	go levels.cleanupStats(cancelCtx)

	return levels
}

func NewLevels(cf *sql.DB, batchSize int, bucketSize time.Duration) *Levels {
	return NewLevelsEx(cf, batchSize, bucketSize, 2*time.Second, bucketSize)
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

func (l *Levels) DifficultyEx(fingerprint string, p *dbgen.Property, tnow time.Time) (uint8, Stats) {
	l.recordAccess(fingerprint, p, tnow)

	stats := l.counts.FetchStats(p.ID, fingerprint, tnow)
	if !stats.HasProperty {
		l.backfillProperty(p)
	}
	if !stats.HasUser {
		l.backfillUser(p, fingerprint)
	}

	decayRate := decayRateForLevel(p.DifficultyGrowth)
	sum := stats.Sum(decayRate)
	minDifficulty := minDifficultyForLevel(p.DifficultyLevel)

	return requestsToDifficulty(sum, minDifficulty, p.DifficultyGrowth), stats
}

func (l *Levels) Difficulty(fingerprint string, p *dbgen.Property) uint8 {
	tnow := time.Now().UTC()
	d, _ := l.DifficultyEx(fingerprint, p, tnow)
	return d
}

func (l *Levels) backfillProperty(p *dbgen.Property) {
	br := &backfillRequest{
		UserID:      p.UserID.Int32,
		PropertyID:  p.ID,
		Fingerprint: "",
	}
	l.backfillChan <- br
}

func (l *Levels) backfillUser(p *dbgen.Property, fingerprint string) {
	br := &backfillRequest{
		UserID:      p.UserID.Int32,
		PropertyID:  p.ID,
		Fingerprint: fingerprint,
	}
	l.backfillChan <- br
}

func (l *Levels) recordAccess(fingerprint string, p *dbgen.Property, tnow time.Time) {
	if (p == nil) || !p.ExternalID.Valid {
		return
	}

	ar := &accessRecord{
		Fingerprint: fingerprint,
		UserID:      p.UserID.Int32,
		PropertyID:  p.ID,
		Timestamp:   tnow,
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

	slog.DebugContext(ctx, "Starting cleaning up stats")

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
				deleteChunk *= 2
			}
		}
	}

	slog.DebugContext(ctx, "Finished cleaning up stats")
}

func (l *Levels) cleanupStats(ctx context.Context) {
	// bucket window is currently 5 minutes so it may change at a minute step
	// here we have 30 seconds since 1 minute sounds way too long
	cleanupStatsImpl(ctx, 30*time.Second, 100 /*chunkSize*/, func(t time.Time, size int) int {
		return l.counts.Cleanup(t, 5 /*buckets*/, size)
	})
}

func (l *Levels) Reset() {
	l.counts.Clear()
}

func (l *Levels) backfillDifficulty(ctx context.Context, cacheDuration time.Duration) {
	slog.DebugContext(ctx, "Backfilling difficulty")

	cache := make(map[string]time.Time)
	const maxCacheSize = 250
	lastCleanupTime := time.Now()

	for r := range l.backfillChan {
		blog := slog.With("pid", r.PropertyID, "fingerprint", r.Fingerprint)
		cacheKey := r.Key()
		tnow := time.Now()
		if t, ok := cache[cacheKey]; ok && tnow.Sub(t) <= cacheDuration {
			blog.WarnContext(ctx, "Skipping duplicate backfill request", "time", t)
			continue
		}

		var counts []*TimeCount
		var err error
		queryProperty := len(r.Fingerprint) == 0
		if queryProperty {
			counts, err = queryPropertyStats(ctx, l.clickhouse, r, l.counts.bucketSize)
		} else {
			counts, err = queryUserStats(ctx, l.clickhouse, r, l.counts.bucketSize)
		}

		if err != nil {
			blog.ErrorContext(ctx, "Failed to backfill stats", common.ErrAttr(err))
			continue
		}

		cache[cacheKey] = tnow

		if len(counts) > 0 {
			blog.DebugContext(ctx, "Backfilling requests counts", "counts", len(counts))
			if queryProperty {
				l.counts.BackfillProperty(r.PropertyID, counts)
			} else {
				l.counts.BackfillUser(r.PropertyID, r.Fingerprint, counts)
			}
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

func queryPropertyStats(ctx context.Context, db *sql.DB, r *backfillRequest, bucketSize time.Duration) ([]*TimeCount, error) {
	timeFrom := time.Now().UTC().Add(-time.Duration(5) * bucketSize)
	query := `SELECT timestamp, sum(count) as count
FROM %s FINAL
WHERE user_id = {user_id:UInt32} AND property_id = {property_id:UInt32} AND timestamp >= {timestamp:DateTime}
GROUP BY timestamp
ORDER BY timestamp`
	rows, err := db.Query(fmt.Sprintf(query, tableName5m),
		clickhouse.Named("user_id", strconv.Itoa(int(r.UserID))),
		clickhouse.Named("property_id", strconv.Itoa(int(r.PropertyID))),
		clickhouse.Named("timestamp", timeFrom.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute backfill stats query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*TimeCount, 0)

	for rows.Next() {
		bc := &TimeCount{}
		if err := rows.Scan(&bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from backfill property stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	return results, nil
}

func queryUserStats(ctx context.Context, db *sql.DB, r *backfillRequest, bucketSize time.Duration) ([]*TimeCount, error) {
	timeFrom := time.Now().UTC().Add(-bucketSize)
	query := `SELECT timestamp, count
FROM %s FINAL
WHERE user_id = {user_id:UInt32} AND property_id = {property_id:UInt32} AND fingerprint = {fingerprint:String} AND timestamp >= {timestamp:DateTime}
ORDER BY timestamp`
	rows, err := db.Query(fmt.Sprintf(query, tableName5m),
		clickhouse.Named("user_id", strconv.Itoa(int(r.UserID))),
		clickhouse.Named("property_id", strconv.Itoa(int(r.PropertyID))),
		clickhouse.Named("fingerprint", r.Fingerprint),
		clickhouse.Named("timestamp", timeFrom.Format(time.DateTime)))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to execute backfill user stats query", common.ErrAttr(err))
		return nil, err
	}

	defer rows.Close()

	results := make([]*TimeCount, 0)

	for rows.Next() {
		bc := &TimeCount{}
		if err := rows.Scan(&bc.Timestamp, &bc.Count); err != nil {
			slog.ErrorContext(ctx, "Failed to read row from backfill stats query", common.ErrAttr(err))
			return nil, err
		}
		results = append(results, bc)
	}

	return results, nil
}

func (l *Levels) processAccessLog(ctx context.Context, delay time.Duration) {
	var batch []*accessRecord
	slog.DebugContext(ctx, "Processing access log")

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
				if err := l.processAccessLogBatch(ctx, batch); err == nil {
					slog.DebugContext(ctx, "Inserted batch of access records", "size", len(batch))
					batch = []*accessRecord{}
				} else {
					slog.ErrorContext(ctx, "Failed to process batch", common.ErrAttr(err))
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				if err := l.processAccessLogBatch(ctx, batch); err == nil {
					slog.DebugContext(ctx, "Inserted batch of access records after delay", "size", len(batch))
					batch = []*accessRecord{}
				} else {
					slog.ErrorContext(ctx, "Failed to process batch", common.ErrAttr(err))
				}
			}
		}
	}

	slog.InfoContext(ctx, "Finished processing access log")
}

func (l *Levels) processAccessLogBatch(ctx context.Context, records []*accessRecord) error {
	scope, err := l.clickhouse.Begin()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to begin batch insert", common.ErrAttr(err))
		return err
	}

	batch, err := scope.Prepare(fmt.Sprintf("INSERT INTO %s", l.tableName))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to prepare insert query", common.ErrAttr(err))
		return err
	}

	for i, r := range records {
		_, err = batch.Exec(r.UserID, r.PropertyID, r.Fingerprint, r.Timestamp)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to exec insert for record", common.ErrAttr(err), "index", i)
			return err
		}
	}

	return scope.Commit()
}

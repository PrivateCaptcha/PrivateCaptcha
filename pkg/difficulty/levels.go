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
	userBucketsCacheFilename   = "user_buckets.gob"
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

/*
* On the client side, difficulty is the logarithmic encoding of work. So work(d) = 2^(d/8)
* (1 step of difficulty change incurrs 2^(1/8) jump in computations => for +100% computations we do +8 difficulty)
* Generally, work multiplier = 2^(delta_D / 8), where (delta_D is change in difficulty) => (solving for delta_D)
* delta_D = 8 * log2(work multiplier)
*
* So to generalize, we will have
* final_difficulty = base_difficulty + 8 * log2(extra work multiplier)
*
* In our calculations:
* - user bucket level (u) is "requests above the leak target (that have not yet decayed)"
* - property bucket level (p) is "accumulated deviation" from the running mean
*
* To add `u + p` directly for our calculation we need to normalize them. (u/U8) and (p/P8), where
* U8/P8 - how many user/property requests (over the limit) produce +100% computations.
* e.g. If (u == U8), then each user request contributs one "doubling" unit of difficulty
*
* To encode difficulty growth we use multiplier `g`: delta_D = 8 * g * log2(work multiplier)
*
* Finally, "The Model" for `work multiplier` is:
* F(u,p) = (1 + u/U8)^(g * wu) * (1 + p/P8)^(g * wp)
* where `wp` and `wu` are respective weights of how much user and property levels measure
* Both are mulplied to make user and property levels cross-dependent (e.g. a suspicious user during a property-wide spike affects more)
* so if you "open" logarithm, it results in `wu*(1 + u/U8) + wp*(1 + p/P8)`
*
* In terms of "slow" growth, we currently select y=log2(1+x) function
*
* Knobs:
* P8 is kind of the most imporant one. We define P8 = K * LeakRate, where
* LeakRate is expected number of requests per interval, K is the "model" constant - number of normal bucket equivalents
* that our leaky bucket accumulates. So K*LeakRate is kind of "accumulated excess measured in normal property buckets".
* e.g. 4*LeakRate => "property has accumulated excess equal to about 4 normal buckets of traffic during leak interval"
* Also: we have to cap P8 from below because new properties don't yet have good learned data.
 */
func requestsToDifficulty(
	userLevel leakybucket.TLevel,
	propertyLevel leakybucket.TLevel,
	propertyLeakRate float64,
	propertyBucketSize time.Duration,
	baseDifficulty float64,
	growth dbgen.DifficultyGrowth,
) uint8 {
	if baseDifficulty >= 255.0 {
		return 255
	}

	g := growthMultiplier(growth)
	if g <= 0 {
		return uint8(min(baseDifficulty, 255.0))
	}

	const (
		userRef            = 8.0  // U8
		propertyMinRPS     = 0.25 // P8 cap utility
		propertyRefBuckets = 4    // default value for `K` in the formula - number of accumulated "normal buckets" in excess
		userWeight         = 1.0  // requester user contributes each data point
		propertyWeight     = 0.5  // property anomaly has moderate immediate effect on particular user
	)

	propertyRefMin := propertyMinRPS * propertyBucketSize.Seconds()
	propertyRefLeak := propertyRefBuckets * propertyLeakRate
	// we use sqrt to make max(propertyRefMin, propertyRefLeak) smoother
	propertyRef := math.Sqrt(propertyRefMin*propertyRefMin + propertyRefLeak*propertyRefLeak)

	u := float64(userLevel)
	p := float64(propertyLevel)

	userPressure := userWeight * log2p(u/userRef)
	propertyPressure := propertyWeight * log2p(p/propertyRef)

	delta := 8.0 * g * (userPressure + propertyPressure)
	difficulty := baseDifficulty + math.Round(delta)

	if difficulty >= 255.0 {
		return 255
	}

	return uint8(difficulty)
}

func log2p(x float64) float64 {
	return math.Log1p(x) / math.Ln2
}

func growthMultiplier(level dbgen.DifficultyGrowth) float64 {
	switch level {
	case dbgen.DifficultyGrowthSlow:
		return 0.70710678
	case dbgen.DifficultyGrowthMedium:
		return 1.0
	case dbgen.DifficultyGrowthFast:
		return 1.41421356
	case dbgen.DifficultyGrowthConstant:
		return 0.0
	default:
		return 1.0
	}
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

func (l *Levels) Stop() {
	slog.Debug("Shutting down levels routines")
	l.accessLogCancel()
}

func (l *Levels) Shutdown() {
	close(l.accessChan)
	close(l.backfillChan)
}

func (l *Levels) DifficultyEx(ctx context.Context, fingerprint common.TFingerprint, p Property, tnow time.Time) (uint8, leakybucket.TLevel, error) {
	err := l.recordAccess(ctx, fingerprint, p, tnow)

	minDifficulty := float64(max(int16(common.MinDifficultyLevel), min(p.Level(), int16(common.MaxDifficultyLevel))))

	propertyAddResult := l.propertyBuckets.Add(p.ID(), 1, tnow)
	if !propertyAddResult.Found {
		if perr := l.backfillProperty(ctx, p); perr != nil {
			// yes, we override, because it's not that important
			err = perr
		}
	}

	userAddResult := l.userBuckets.Add(fingerprint, 1, tnow)

	difficulty := requestsToDifficulty(userAddResult.CurrLevel,
		propertyAddResult.CurrLevel,
		propertyAddResult.LeakRate,
		l.propertyBuckets.LeakInterval(),
		minDifficulty,
		p.Growth(),
	)

	return difficulty, propertyAddResult.CurrLevel, err
}

func (l *Levels) Difficulty(ctx context.Context, fingerprint common.TFingerprint, p Property, tnow time.Time) uint8 {
	diff, _, _ := l.DifficultyEx(ctx, fingerprint, p, tnow)
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
		RuleID:      0,
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
		RuleID:      p.RuleID(),
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

func (l *Levels) SaveCache(ctx context.Context, dir string) error {
	const cachePersistSize = 1_000
	return l.userBuckets.SaveCache(ctx, dir, userBucketsCacheFilename, cachePersistSize, time.Now())
}

func (l *Levels) LoadCache(ctx context.Context, dir string) error {
	return l.userBuckets.LoadCache(ctx, dir, userBucketsCacheFilename)
}

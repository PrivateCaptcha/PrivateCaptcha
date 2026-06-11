//go:build simulation

package difficulty

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
)

type SimDist string

const (
	SimDistConst   SimDist = "const"
	SimDistPoisson SimDist = "poisson"
	SimDistBurst   SimDist = "burst"
)

type SimFormula string

const (
	SimFormulaV1 SimFormula = "v1"
	SimFormulaV2 SimFormula = "v2"
)

type SimRequestModel struct {
	Dist       SimDist
	Rate       float64 // expected requests per simulation step
	BurstRate  float64 // expected requests per simulation step during [BurstStart, BurstEnd)
	BurstStart int
	BurstEnd   int
}

type SimFormulaConfig struct {
	UserRef            float64 // U8: user bucket level that adds about +8 difficulty in normal mode before weights
	MinExpectedRPS     float64 // minimum expected property RPS used to derive the smooth P8 floor from bucket size
	PropertyRefBuckets float64 // K: normal-property-bucket equivalents used to derive P8 from leak rate
	UserWeight         float64
	PropertyWeight     float64
	CrossWeight        float64 // optional interaction: userPressure*propertyPressure
}

type SimConfig struct {
	Steps              int
	Step               time.Duration
	PropertyBucketSize time.Duration
	BaseDifficulty     float64
	Growth             dbgen.DifficultyGrowth
	Formula            SimFormula
	Debug              bool

	EnableUser              bool
	EnableProperty          bool
	UserContributesProperty bool

	UserModel     SimRequestModel
	PropertyModel SimRequestModel
	FormulaConfig SimFormulaConfig

	Seed int64
}

type simGenerator struct {
	model SimRequestModel
	rng   *rand.Rand
	carry float64
}

func newSimGenerator(model SimRequestModel, seed int64) *simGenerator {
	return &simGenerator{model: model, rng: rand.New(rand.NewSource(seed))}
}

func (g *simGenerator) next(step int) leakybucket.TLevel {
	rate := g.model.Rate
	if g.model.Dist == SimDistBurst && step >= g.model.BurstStart && step < g.model.BurstEnd {
		rate = g.model.BurstRate
	}

	if rate <= 0 {
		return 0
	}

	switch g.model.Dist {
	case SimDistConst:
		g.carry += rate
		n := math.Floor(g.carry)
		g.carry -= n
		return clampTLevel(n)
	case SimDistPoisson, SimDistBurst:
		return samplePoisson(rate, g.rng)
	default:
		g.carry += rate
		n := math.Floor(g.carry)
		g.carry -= n
		return clampTLevel(n)
	}
}

func samplePoisson(lambda float64, rng *rand.Rand) leakybucket.TLevel {
	if lambda <= 0 {
		return 0
	}

	// Knuth's exact algorithm is fine for small lambdas. For larger values,
	// use a normal approximation to keep large simulations cheap.
	if lambda < 30.0 {
		l := math.Exp(-lambda)
		k := 0
		p := 1.0
		for p > l {
			k++
			p *= rng.Float64()
		}
		return leakybucket.TLevel(max(k-1, 0))
	}

	v := rng.NormFloat64()*math.Sqrt(lambda) + lambda
	if v <= 0 {
		return 0
	}
	return clampTLevel(math.Round(v))
}

func clampTLevel(v float64) leakybucket.TLevel {
	if v <= 0 {
		return 0
	}
	if v >= math.MaxUint32 {
		return math.MaxUint32
	}
	return leakybucket.TLevel(v)
}

func addTLevel(a, b leakybucket.TLevel) leakybucket.TLevel {
	if uint64(a)+uint64(b) > math.MaxUint32 {
		return math.MaxUint32
	}
	return a + b
}

func normalizeSimFormulaConfig(cfg SimFormulaConfig) SimFormulaConfig {
	if cfg.UserRef <= 0 {
		cfg.UserRef = 8.0
	}
	if cfg.MinExpectedRPS <= 0 {
		cfg.MinExpectedRPS = 0.5
	}
	if cfg.PropertyRefBuckets <= 0 {
		cfg.PropertyRefBuckets = 4.0
	}
	if cfg.UserWeight == 0 {
		cfg.UserWeight = 1.0
	}
	if cfg.PropertyWeight == 0 {
		cfg.PropertyWeight = 0.25
	}
	return cfg
}

func candidatePropertyRef(propertyLeakRate float64, propertyBucketSize time.Duration, cfg SimFormulaConfig) float64 {
	cfg = normalizeSimFormulaConfig(cfg)
	refFromLeak := cfg.PropertyRefBuckets * propertyLeakRate
	refFromMinRPS := cfg.MinExpectedRPS * propertyBucketSize.Seconds()
	return math.Sqrt(refFromMinRPS*refFromMinRPS + refFromLeak*refFromLeak)
	//return max(refFromMinRPS, refFromLeak)
}

// candidateDifficultyV1 is the legacy requestsToDifficulty formula that used
// a single combined request/bucket level. Keep it here for simulation-only comparisons.
func candidateDifficultyV1(requests float64, minDifficulty float64, level dbgen.DifficultyGrowth) uint8 {
	if (requests < 1.0) || (level == dbgen.DifficultyGrowthConstant) || (minDifficulty >= 255.0) {
		return uint8(min(minDifficulty, 255.0))
	}

	// Full formula was:
	// y = log2(log2(x**a)) * x**b
	// parameter "a" affects sensitivity to growth.
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

type simV2Breakdown struct {
	GrowthMultiplier float64

	UserRatio     float64
	PropertyRatio float64

	UserRawPressure     float64
	PropertyRawPressure float64

	WeightedUserPressure     float64
	WeightedPropertyPressure float64
	CrossPressure            float64
	TotalPressure            float64

	UserDelta       float64
	PropertyDelta   float64
	CrossDelta      float64
	TotalDelta      float64
	DifficultyFloat float64
	DifficultyByte  uint8
}

func candidateDifficultyV2Breakdown(
	userLevel leakybucket.TLevel,
	propertyLevel leakybucket.TLevel,
	propertyLeakRate float64,
	propertyBucketSize time.Duration,
	baseDifficulty float64,
	growth dbgen.DifficultyGrowth,
	cfg SimFormulaConfig,
) simV2Breakdown {
	cfg = normalizeSimFormulaConfig(cfg)
	propertyRef := candidatePropertyRef(propertyLeakRate, propertyBucketSize, cfg)
	gm := growthMultiplier(growth)

	bd := simV2Breakdown{GrowthMultiplier: gm}
	if cfg.UserRef > 0 {
		bd.UserRatio = float64(userLevel) / cfg.UserRef
	}
	if propertyRef > 0 {
		bd.PropertyRatio = float64(propertyLevel) / propertyRef
	}

	if baseDifficulty >= 255.0 {
		bd.DifficultyFloat = 255.0
		bd.DifficultyByte = 255
		return bd
	}
	if gm <= 0.0 {
		bd.DifficultyFloat = min(max(baseDifficulty, 0), 255)
		bd.DifficultyByte = uint8(bd.DifficultyFloat)
		return bd
	}

	bd.UserRawPressure = log2p(bd.UserRatio)
	bd.PropertyRawPressure = log2p(bd.PropertyRatio)
	bd.WeightedUserPressure = cfg.UserWeight * bd.UserRawPressure
	bd.WeightedPropertyPressure = cfg.PropertyWeight * bd.PropertyRawPressure
	bd.CrossPressure = cfg.CrossWeight * bd.UserRawPressure * bd.PropertyRawPressure
	bd.TotalPressure = bd.WeightedUserPressure + bd.WeightedPropertyPressure + bd.CrossPressure

	bd.UserDelta = 8.0 * gm * bd.WeightedUserPressure
	bd.PropertyDelta = 8.0 * gm * bd.WeightedPropertyPressure
	bd.CrossDelta = 8.0 * gm * bd.CrossPressure
	bd.TotalDelta = 8.0 * gm * bd.TotalPressure
	bd.DifficultyFloat = baseDifficulty + bd.TotalDelta

	difficulty := baseDifficulty + math.Round(bd.TotalDelta)
	if difficulty >= 255.0 {
		bd.DifficultyByte = 255
		bd.DifficultyFloat = min(bd.DifficultyFloat, 255)
		return bd
	}
	if difficulty <= 0 {
		bd.DifficultyByte = 0
		bd.DifficultyFloat = max(bd.DifficultyFloat, 0)
		return bd
	}
	bd.DifficultyByte = uint8(difficulty)
	return bd
}

// candidateDifficultyV2 mirrors the new requestsToDifficulty formula, but keeps
// K/minExpectedRPS/userRef/weights configurable so the simulator can explore them.
func candidateDifficultyV2(
	userLevel leakybucket.TLevel,
	propertyLevel leakybucket.TLevel,
	propertyLeakRate float64,
	propertyBucketSize time.Duration,
	baseDifficulty float64,
	growth dbgen.DifficultyGrowth,
	cfg SimFormulaConfig,
) uint8 {
	return candidateDifficultyV2Breakdown(userLevel, propertyLevel, propertyLeakRate, propertyBucketSize, baseDifficulty, growth, cfg).DifficultyByte
}

// RunSimulation writes tab-separated rows suitable for gnuplot.
//
// Normal columns:
//
//	time_s property_req user_req property_total_req property_level user_level
//	property_leak_rate property_ref v1_difficulty v2_difficulty difficulty
//
// With cfg.Debug=true, these diagnostic columns are appended:
//
//	property_ref_from_leak property_ref_from_min_rps property_excess_buckets
//	property_ratio user_ratio user_raw_pressure property_raw_pressure
//	weighted_user_pressure weighted_property_pressure cross_pressure total_pressure
//	user_delta property_delta cross_delta total_delta_float difficulty_float
//	expected_property_req_step instant_property_rate_ratio instant_property_excess_ratio
//	property_leak_rate_before_add property_leak_rate_after_add
func RunSimulation(out io.Writer, cfg SimConfig) error {
	if cfg.Steps <= 0 {
		cfg.Steps = 600
	}
	if cfg.Step <= 0 {
		cfg.Step = time.Second
	}
	if cfg.PropertyBucketSize <= 0 {
		cfg.PropertyBucketSize = 5 * time.Minute
	}
	if cfg.BaseDifficulty <= 0 {
		cfg.BaseDifficulty = 100
	}
	if cfg.Seed == 0 {
		cfg.Seed = 1
	}
	cfg.FormulaConfig = normalizeSimFormulaConfig(cfg.FormulaConfig)

	levels := NewLevels(nil, 1000, cfg.PropertyBucketSize)
	start := time.Unix(0, 0).UTC()
	propertyID := int32(1)
	fingerprint := common.TFingerprint(1)

	userGen := newSimGenerator(cfg.UserModel, cfg.Seed+101)
	propertyGen := newSimGenerator(cfg.PropertyModel, cfg.Seed+202)

	// VarLeakyBucket initializes leakRate to 1.0. Track the last value returned
	// by AddResult.LeakRate so we can compute and plot P8 even on quiet steps.
	propertyLeakRate := 1.0

	_, _ = fmt.Fprint(out, "time_s\tproperty_req\tuser_req\tproperty_total_req\tproperty_level\tuser_level\tproperty_leak_rate\tproperty_ref\tv1_difficulty\tv2_difficulty\tdifficulty")
	if cfg.Debug {
		_, _ = fmt.Fprint(out, "\tproperty_ref_from_leak\tproperty_ref_from_min_rps\tproperty_excess_buckets\tproperty_ratio\tuser_ratio\tuser_raw_pressure\tproperty_raw_pressure\tweighted_user_pressure\tweighted_property_pressure\tcross_pressure\ttotal_pressure\tuser_delta\tproperty_delta\tcross_delta\ttotal_delta_float\tdifficulty_float\texpected_property_req_step\tinstant_property_rate_ratio\tinstant_property_excess_ratio\tproperty_leak_rate_before_add\tproperty_leak_rate_after_add")
	}
	_, _ = fmt.Fprintln(out)

	for i := 0; i < cfg.Steps; i++ {
		tnow := start.Add(time.Duration(i) * cfg.Step)

		var userReq leakybucket.TLevel
		var propertyReq leakybucket.TLevel
		var propertyTotalReq leakybucket.TLevel

		propertyLeakRateBeforeAdd := propertyLeakRate
		propertyLeakRateAfterAdd := propertyLeakRate

		if cfg.EnableUser {
			userReq = userGen.next(i)
			if userReq > 0 {
				levels.userBuckets.Add(fingerprint, userReq, tnow)
			}
		}

		if cfg.EnableProperty {
			propertyReq = propertyGen.next(i)
			propertyTotalReq = propertyReq
			if cfg.UserContributesProperty {
				propertyTotalReq = addTLevel(propertyTotalReq, userReq)
			}
			if propertyTotalReq > 0 {
				propertyLeakRateBeforeAdd = propertyLeakRate
				addResult := levels.propertyBuckets.Add(propertyID, propertyTotalReq, tnow)
				if addResult.LeakRate > 0 {
					propertyLeakRate = addResult.LeakRate
					propertyLeakRateAfterAdd = addResult.LeakRate
				}
			}
		}

		propertyLevel, _ := levels.propertyBuckets.Level(propertyID, tnow)
		userLevel, _ := levels.userBuckets.Level(fingerprint, tnow)
		combined := addTLevel(propertyLevel, userLevel)
		propertyRef := candidatePropertyRef(propertyLeakRate, cfg.PropertyBucketSize, cfg.FormulaConfig)
		propertyRefFromLeak := cfg.FormulaConfig.PropertyRefBuckets * propertyLeakRate
		propertyRefFromMinRPS := cfg.FormulaConfig.MinExpectedRPS * cfg.PropertyBucketSize.Seconds()

		v1 := candidateDifficultyV1(float64(combined), cfg.BaseDifficulty, cfg.Growth)
		v2Breakdown := candidateDifficultyV2Breakdown(userLevel, propertyLevel, propertyLeakRate, cfg.PropertyBucketSize, cfg.BaseDifficulty, cfg.Growth, cfg.FormulaConfig)
		v2 := v2Breakdown.DifficultyByte

		difficulty := v2
		if cfg.Formula == SimFormulaV1 {
			difficulty = v1
		}

		_, _ = fmt.Fprintf(out, "%d\t%d\t%d\t%d\t%d\t%d\t%.6f\t%.6f\t%d\t%d\t%d",
			int64(tnow.Sub(start).Seconds()),
			propertyReq,
			userReq,
			propertyTotalReq,
			propertyLevel,
			userLevel,
			propertyLeakRate,
			propertyRef,
			v1,
			v2,
			difficulty,
		)

		if cfg.Debug {
			propertyExcessBuckets := 0.0
			if propertyLeakRate > 0 {
				propertyExcessBuckets = float64(propertyLevel) / propertyLeakRate
			}

			expectedPropertyReqStep := 0.0
			if cfg.PropertyBucketSize > 0 {
				expectedPropertyReqStep = propertyLeakRateBeforeAdd * float64(cfg.Step) / float64(cfg.PropertyBucketSize)
			}
			instantPropertyRateRatio := 0.0
			if expectedPropertyReqStep > 0 {
				instantPropertyRateRatio = float64(propertyTotalReq) / expectedPropertyReqStep
			}
			instantPropertyExcessRatio := math.Max(0.0, instantPropertyRateRatio-1.0)

			_, _ = fmt.Fprintf(out, "\t%.6f\t%.0f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f\t%.6f",
				propertyRefFromLeak,
				propertyRefFromMinRPS,
				propertyExcessBuckets,
				v2Breakdown.PropertyRatio,
				v2Breakdown.UserRatio,
				v2Breakdown.UserRawPressure,
				v2Breakdown.PropertyRawPressure,
				v2Breakdown.WeightedUserPressure,
				v2Breakdown.WeightedPropertyPressure,
				v2Breakdown.CrossPressure,
				v2Breakdown.TotalPressure,
				v2Breakdown.UserDelta,
				v2Breakdown.PropertyDelta,
				v2Breakdown.CrossDelta,
				v2Breakdown.TotalDelta,
				v2Breakdown.DifficultyFloat,
				expectedPropertyReqStep,
				instantPropertyRateRatio,
				instantPropertyExcessRatio,
				propertyLeakRateBeforeAdd,
				propertyLeakRateAfterAdd,
			)
		}
		_, _ = fmt.Fprintln(out)
	}

	return nil
}

package difficulty

import (
	"math"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/leakybucket"
)

type Algorithm interface {
	Difficulty(propertyData *leakybucket.AddResult, userData *leakybucket.AddResult, property Property) uint8
}

// Unlike SaaS version, self-hosting installation has no privilege of having enriched (or even aggregated) global
// data on requesters at large, so we weight property patterns more than users. SaaS version is waay more nuanced.
type SelfHostingAlgorithm struct {
	propertyRefMin float64
}

func NewDifficultyAlgorithm(propertyBucketSize time.Duration) *SelfHostingAlgorithm {
	// TODO: Make a self-hosting env config for algorithm tuning
	const propertyMinRPS = 0.25 /*average rps for self-hosting?*/
	return &SelfHostingAlgorithm{
		propertyRefMin: propertyMinRPS * propertyBucketSize.Seconds(),
	}
}

func (a *SelfHostingAlgorithm) Difficulty(propertyData *leakybucket.AddResult, userData *leakybucket.AddResult, property Property) uint8 {
	minDifficulty := float64(max(int16(common.MinDifficultyLevel), min(property.Level(), int16(common.MaxDifficultyLevel))))

	growth := growthMultiplier(property.Growth())
	if growth <= 0 {
		return uint8(min(minDifficulty, 255.0))
	}

	return a.requestsToDifficulty(userData.CurrLevel,
		propertyData.CurrLevel,
		propertyData.LeakRate,
		minDifficulty,
		growth,
	)
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
* Finally, "The Model" for `work multiplier` (simplified) is:
* F(u, p) = (1 + u/U8)^(g * wu) * (1 + p/P8)^(g * wp)
* where `wp` and `wu` are respective weights of how much user and property levels measure
* Both are multiplied to make user and property levels cross-dependent (e.g. a suspicious user during a property-wide spike affects more)
* so if you "open" logarithm, it results in `wu*(1 + u/U8) + wp*(1 + p/P8)`
*
* Also we don't simply "weight" them. To make sure we don't exceed 255, we calculate actual available "headspace"
* (255 - minDifficulty) and fit it in.
*
* In terms of "slow" growth, we currently select y=log2(1+x) function.
*
* Knobs:
* P8 is kind of the most imporant one - how to scale property level. We define P8 = K * LeakRate, where
* LeakRate is expected number of requests per interval, K is the "model" constant - number of normal bucket equivalents
* that our var leaky bucket "anomaly" level accumulates. So K*LeakRate is kind of "accumulated excess measured in normal property buckets".
* e.g. 4*LeakRate => "property has accumulated excess equal to about 4 normal buckets of traffic during leak interval"
* Also: we have to cap P8 from below because fresh properties don't yet have good learned data (leak rate), but choosing
* good lower cap is not obvious either. We ground it with "expected" minimal RPS for the property (x property bucket size).
 */

func (a *SelfHostingAlgorithm) requestsToDifficulty(
	userLevel leakybucket.TLevel,
	propertyLevel leakybucket.TLevel,
	propertyLeakRate float64,
	baseDifficulty float64,
	growth float64,
) uint8 {
	if baseDifficulty >= 255.0 {
		return 255
	}

	headroomDifficulty := 255.0 - baseDifficulty
	if headroomDifficulty <= 0 {
		return 255
	}

	const (
		userRef            = 8.0 // U8
		propertyRefBuckets = 4.0 // K
		wu                 = 1.0 // weight of user component
		wp                 = 1.0 // weight of property component
		totalWeight        = wu + wp
	)

	propertyRefLeak := propertyRefBuckets * propertyLeakRate
	propertyRef := max(a.propertyRefMin, propertyRefLeak)

	u := float64(userLevel)
	p := float64(propertyLevel)

	// Raw pressure is measured in "work doubling units" (e.g. userRawPressure == 1 means "user component asks +100 work")
	userRawPressure := log2p(u/userRef) * wu / totalWeight
	propertyRawPressure := log2p(p/propertyRef) * wp / totalWeight
	rawPressure := userRawPressure + propertyRawPressure

	headroom := headroomDifficulty / 8.0
	effectivePressure := math.Min(headroom, growth*rawPressure)
	difficulty := baseDifficulty + math.Round(8.0*effectivePressure)
	if difficulty >= 255.0 {
		return 255
	}

	return uint8(difficulty)
}

func log2p(x float64) float64 {
	if x <= 0 {
		return 0
	}
	return math.Log1p(x) / math.Ln2
}

func growthMultiplier(level dbgen.DifficultyGrowth) float64 {
	switch level {
	case dbgen.DifficultyGrowthSlow:
		return 0.5
	case dbgen.DifficultyGrowthMedium:
		return 1.0
	case dbgen.DifficultyGrowthFast:
		return 1.5
	case dbgen.DifficultyGrowthConstant:
		return 0.0
	default:
		return 1.0
	}
}

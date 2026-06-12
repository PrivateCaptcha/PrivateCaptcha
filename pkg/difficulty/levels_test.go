package difficulty

import (
	"fmt"
	"testing"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func TestDifficultyFormula(t *testing.T) {
	testCases := []struct {
		// note that user level is kind of "direct" user level
		userLevel uint32
		// and property level is the "anomaly" level
		propertyLevel uint32
		minDifficulty float64
		growthLevel   dbgen.DifficultyGrowth
		expected      uint8
	}{
		{0, 0, 10, dbgen.DifficultyGrowthMedium, 10},
		{0, 0, 100, dbgen.DifficultyGrowthMedium, 100},
		{0, 1, 100, dbgen.DifficultyGrowthMedium, 100},
		{0, 10, 100, dbgen.DifficultyGrowthMedium, 101},
		{1, 0, 100, dbgen.DifficultyGrowthMedium, 101},
		{0, 100, 100, dbgen.DifficultyGrowthMedium, 105},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("difficulty_%v", i), func(t *testing.T) {
			a := NewDifficultyAlgorithm(5 * time.Minute)
			growth := growthMultiplier(tc.growthLevel)
			actual := a.requestsToDifficulty(tc.userLevel, tc.propertyLevel, 1.0, tc.minDifficulty, growth)
			if actual != tc.expected {
				t.Errorf("Actual difficulty (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

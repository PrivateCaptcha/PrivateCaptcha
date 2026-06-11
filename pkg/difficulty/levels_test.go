package difficulty

import (
	"fmt"
	"testing"
	"time"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func TestDifficultyFormula(t *testing.T) {
	testCases := []struct {
		userLevel     uint32
		propertyLevel uint32
		minDifficulty float64
		growthLevel   dbgen.DifficultyGrowth
		expected      uint8
	}{
		{0, 0, 10, dbgen.DifficultyGrowthMedium, 10},
		{0, 0, 100, dbgen.DifficultyGrowthMedium, 100},
		{0, 1, 100, dbgen.DifficultyGrowthMedium, 100},
		{0, 100, 100, dbgen.DifficultyGrowthMedium, 105},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("difficulty_%v", i), func(t *testing.T) {
			actual := requestsToDifficulty(tc.userLevel, tc.propertyLevel, 1.0, 5*time.Minute, tc.minDifficulty, tc.growthLevel)
			if actual != tc.expected {
				t.Errorf("Actual difficulty (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

package difficulty

import (
	"fmt"
	"testing"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

func TestDifficultyFormula(t *testing.T) {
	testCases := []struct {
		level         float64
		minDifficulty uint8
		growthLevel   dbgen.DifficultyGrowth
		expected      uint8
	}{
		{0.0, 10, dbgen.DifficultyGrowthMedium, 10},
		{0.0, 100, dbgen.DifficultyGrowthMedium, 100},
		{1.0, 100, dbgen.DifficultyGrowthMedium, 100},
		{3.0, 100, dbgen.DifficultyGrowthMedium, 101},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("difficulty_%v", i), func(t *testing.T) {
			actual := requestsToDifficulty(tc.level, tc.minDifficulty, tc.growthLevel)
			if actual != tc.expected {
				t.Errorf("Actual difficulty (%v) is different from expected (%v)", actual, tc.expected)
			}
		})
	}
}

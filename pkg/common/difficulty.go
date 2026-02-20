package common

type DifficultyLevel uint8

const (
	// difficulty scales on an exponential curve defined by 2^(d/8) so 1 step of
	// difficulty change incurrs 2^(1/8) = 1.0905077 jump in computations (9.05%)
	// so to get +100% computations we use 8 difficulty steps (so +16 steps is 300%)
	DifficultyDelta                       = 16
	DifficultyLevelSmall  DifficultyLevel = 136
	DifficultyLevelMedium DifficultyLevel = DifficultyLevelSmall + DifficultyDelta
	DifficultyLevelHigh   DifficultyLevel = DifficultyLevelMedium + DifficultyDelta
	MaxDifficultyLevel    DifficultyLevel = 255
	MinDifficultyLevel    DifficultyLevel = 1
)

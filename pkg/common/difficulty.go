package common

type DifficultyLevel uint8

const (
	// NOTE: We want them equally spaced
	DifficultyDelta                       = 15
	DifficultyLevelSmall  DifficultyLevel = 80
	DifficultyLevelMedium DifficultyLevel = DifficultyLevelSmall + DifficultyDelta
	DifficultyLevelHigh   DifficultyLevel = DifficultyLevelMedium + DifficultyDelta
	MaxDifficultyLevel    DifficultyLevel = 255
)

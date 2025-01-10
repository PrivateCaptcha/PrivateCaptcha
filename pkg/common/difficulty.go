package common

type DifficultyLevel uint8

const (
	// NOTE: We want them equally spaced
	DifficultyLevelSmall  DifficultyLevel = 80
	DifficultyLevelMedium DifficultyLevel = 95
	DifficultyLevelHigh   DifficultyLevel = 110
	MaxDifficultyLevel    DifficultyLevel = 255
)

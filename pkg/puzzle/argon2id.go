package puzzle

import (
	"context"
	"encoding/binary"
	"errors"
	"math"

	"golang.org/x/crypto/argon2"
)

const (
	argon2IDPasses       = uint32(1)
	argon2IDParallelism  = uint8(1)
	argon2IDTagLength    = uint32(32)
	argon2IDMemoryKiB    = uint32(16 * 1024)
	argon2IDPasswordSize = 128
	// The v2 profile uses eight lanes and maps logical difficulty 152 to wire difficulty 24.
	Argon2IDMemoryKiB             = argon2IDMemoryKiB
	Argon2IDSolutionsCount        = 8
	Argon2IDDifficultyOffset      = uint8(128)
	Argon2IDMinimumWireDifficulty = uint8(24)
)

var ErrInvalidArgon2IDInput = errors.New("invalid Argon2id input")

func Argon2IDWireDifficulty(logical uint8) (uint8, bool) {
	if logical < Argon2IDDifficultyOffset+Argon2IDMinimumWireDifficulty {
		return 0, false
	}
	return logical - Argon2IDDifficultyOffset, true
}

func validateArgon2IDParameters(canonicalBody []byte, memoryKiB uint32) error {
	if len(canonicalBody) != puzzleV2Size {
		return ErrInvalidArgon2IDInput
	}
	if memoryKiB != argon2IDMemoryKiB {
		return ErrInvalidArgon2IDInput
	}
	return nil
}

// Argon2IDCandidateValue hashes one exact solution nonce against a canonical v2 puzzle body.
func Argon2IDCandidateValue(nonce, canonicalBody []byte, memoryKiB uint32) (uint32, error) {
	if len(nonce) != SolutionLength {
		return 0, ErrInvalidArgon2IDInput
	}
	if err := validateArgon2IDParameters(canonicalBody, memoryKiB); err != nil {
		return 0, err
	}

	var password [argon2IDPasswordSize]byte
	copy(password[:], canonicalBody)
	copy(password[argon2IDPasswordSize-SolutionLength:], nonce)
	tag := argon2.IDKey(password[:], password[:16], argon2IDPasses, memoryKiB, argon2IDParallelism, argon2IDTagLength)
	return binary.LittleEndian.Uint32(tag), nil
}

func argon2IDThresholdFromDifficulty(difficulty uint8) uint32 {
	return uint32(math.Pow(2, (255.999-float64(difficulty))/8.0))
}

// VerifyArgon2ID verifies solutions sequentially with the supplied profile memory size.
func (s *Solutions) VerifyArgon2ID(ctx context.Context, canonicalBody []byte, difficulty uint8, memoryKiB uint32) (int, error) {
	if s == nil || len(s.Buffer)%SolutionLength != 0 {
		return 0, ErrInvalidArgon2IDInput
	}
	if err := validateArgon2IDParameters(canonicalBody, memoryKiB); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	validSolutions := 0
	threshold := argon2IDThresholdFromDifficulty(difficulty)
	for start := 0; start < len(s.Buffer); start += SolutionLength {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		value, err := Argon2IDCandidateValue(s.Buffer[start:start+SolutionLength], canonicalBody, memoryKiB)
		if err != nil {
			return 0, err
		}
		if value <= threshold {
			validSolutions++
		}
	}

	return validSolutions, nil
}

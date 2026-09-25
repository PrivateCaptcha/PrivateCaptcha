package puzzle

import (
	"context"
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/blake2b"
)

type ComputeSolver struct {
}

var ErrArgon2IDExhausted = errors.New("Argon2id nonce space exhausted")

func solveArgon2IDOne(ctx context.Context, body []byte, lane byte, threshold uint32, limit uint64, hash func([]byte, []byte, uint32) (uint32, error)) ([]byte, error) {
	nonce := make([]byte, SolutionLength)
	nonce[0] = lane
	for counter := uint64(0); counter < limit; counter++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		binary.BigEndian.PutUint32(nonce[4:], uint32(counter))
		value, err := hash(nonce, body, Argon2IDMemoryKiB)
		if err != nil {
			return nil, err
		}
		if value <= threshold {
			return nonce, nil
		}
	}
	return nil, ErrArgon2IDExhausted
}

func (s *ComputeSolver) solveArgon2ID(ctx context.Context, p Puzzle, body []byte) (*Solutions, error) {
	if len(body) != puzzleV2Size || p.SolutionsCount() != Argon2IDSolutionsCount {
		return nil, ErrInvalidArgon2IDInput
	}
	started := time.Now()
	solutions := emptySolutions(Argon2IDSolutionsCount)
	threshold := argon2IDThresholdFromDifficulty(p.Difficulty())
	for lane := range Argon2IDSolutionsCount {
		nonce, err := solveArgon2IDOne(ctx, body, byte(lane), threshold, 1<<32, Argon2IDCandidateValue)
		if err != nil {
			return nil, err
		}
		copy(solutions.Buffer[lane*SolutionLength:], nonce)
	}
	solutions.Metadata.elapsedMillis = uint32(time.Since(started).Milliseconds())
	return solutions, nil
}

func (s *ComputeSolver) solveOne(ctx context.Context, buf []byte, threshold uint32, solution []byte) error {
	nonce := buf[len(buf)-4:]

	for i := 0; i < 256; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		nonce[0] = byte(i)

		for j := 0; j < 256; j++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			nonce[1] = byte(j)

			for k := 0; k < 256; k++ {
				nonce[2] = byte(k)

				for l := 0; l < 256; l++ {
					nonce[3] = byte(l)

					hash := blake2b.Sum256(buf)
					resultInt := binary.LittleEndian.Uint32(hash[:4])

					if resultInt <= threshold {
						copy(solution, buf[len(buf)-SolutionLength:])
						return nil
					}
				}
			}
		}
	}

	clear(solution)
	return nil
}

func normalizePuzzleBuffer(buf []byte) []byte {
	if len(buf) < PuzzleBytesLength {
		extended := make([]byte, PuzzleBytesLength)
		copy(extended, buf)
		buf = extended
	}

	return buf
}

func (s *ComputeSolver) Solve(ctx context.Context, p Puzzle) (*Solutions, error) {
	if p.IsZero() && p.Challenge() == ChallengeBlake2b {
		return emptySolutions(p.SolutionsCount()), nil
	}

	buf, err := p.MarshalBinary()
	if err != nil {
		return nil, err
	}
	if p.Challenge() == ChallengeArgon2ID {
		return s.solveArgon2ID(ctx, p, buf)
	}

	buf = normalizePuzzleBuffer(buf)

	threshold := thresholdFromDifficulty(p.Difficulty())
	startTime := time.Now()
	count := p.SolutionsCount()

	numWorkers := min(count, runtime.GOMAXPROCS(0))

	buffer := make([]byte, count*SolutionLength)
	var next atomic.Int64
	var wg sync.WaitGroup

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerBuf := make([]byte, len(buf))
			copy(workerBuf, buf)
			for {
				index := int(next.Add(1)) - 1
				if index >= count {
					return
				}
				workerBuf[len(buf)-SolutionLength] = byte(index)
				if err := s.solveOne(ctx, workerBuf, threshold, buffer[index*SolutionLength:(index+1)*SolutionLength]); err != nil {
					return
				}
			}
		}()
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	elapsed := time.Since(startTime)
	return &Solutions{
		Buffer: buffer,
		Metadata: &Metadata{
			errorCode:     0,
			elapsedMillis: uint32(elapsed.Milliseconds()),
			wasmFlag:      false,
		},
	}, nil
}

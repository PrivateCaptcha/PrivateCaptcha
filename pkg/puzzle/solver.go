package puzzle

import (
	"context"
	"encoding/binary"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/blake2b"
)

type ComputeSolver struct {
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
	if p.IsZero() {
		return emptySolutions(p.SolutionsCount()), nil
	}

	buf, err := p.MarshalBinary()
	if err != nil {
		return nil, err
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

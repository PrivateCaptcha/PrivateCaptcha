package puzzle

import (
	"context"
	"encoding/binary"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"golang.org/x/crypto/blake2b"
)

type ComputeSolver struct {
}

func (s *ComputeSolver) solveOne(ctx context.Context, buf []byte, threshold uint32) ([]byte, error) {
	size := len(buf)

	h, err := blake2b.New256(nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create hasher", common.ErrAttr(err))
		return nil, err
	}

	var hash [blake2b.Size256]byte

	for i := 0; i < 256; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		buf[size-1-3] = byte(i)

		for j := 0; j < 256; j++ {
			buf[size-1-2] = byte(j)

			for k := 0; k < 256; k++ {
				buf[size-1-1] = byte(k)

				for l := 0; l < 256; l++ {
					buf[size-1-0] = byte(l)

					h.Reset()
					if _, err := h.Write(buf); err != nil {
						slog.ErrorContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(err))
						return nil, err
					}
					_ = h.Sum(hash[:0])

					resultInt := binary.LittleEndian.Uint32(hash[:4])

					if resultInt <= threshold {
						// Return a copy so we don't accidentally leak the underlying array
						sol := make([]byte, SolutionLength)
						copy(sol, buf[size-SolutionLength:])
						return sol, nil
					}
				}
			}
		}
	}

	return make([]byte, SolutionLength), nil
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
		return emptySolutions(max(p.SolutionsCount(), solutionsCount)), nil
	}

	buf, err := p.MarshalBinary()
	if err != nil {
		return nil, err
	}

	buf = normalizePuzzleBuffer(buf)

	threshold := thresholdFromDifficulty(p.Difficulty())
	startTime := time.Now()
	count := p.SolutionsCount()

	numWorkers := runtime.NumCPU()

	type workItem struct {
		index int
		data  []byte
	}

	buffer := make([]byte, count*SolutionLength)
	jobs := make(chan workItem, count)
	var wg sync.WaitGroup

	// Spawn fixed worker pool
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if sol, err := s.solveOne(ctx, job.data, threshold); err == nil {
					// Each job writes to its own slice of buffer
					copy(buffer[job.index*SolutionLength:], sol)
				}
			}
		}()
	}

	// Feed work
	for i := 0; i < count; i++ {
		bufCopy := make([]byte, len(buf))
		copy(bufCopy, buf)
		bufCopy[len(buf)-SolutionLength] = byte(i)
		jobs <- workItem{index: i, data: bufCopy}
	}
	close(jobs)
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

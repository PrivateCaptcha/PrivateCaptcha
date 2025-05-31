package puzzle

import (
	"bytes"
	"encoding/binary"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/crypto/blake2b"
)

type Solver struct {
}

func (s *Solver) solveOne(buf []byte, threshold uint32) []byte {
	size := len(buf)
	for i := 0; i < 256; i++ {
		buf[size-1-3] = byte(i)

		for j := 0; j < 256; j++ {
			buf[size-1-2] = byte(j)

			for k := 0; k < 256; k++ {
				buf[size-1-1] = byte(k)

				for l := 0; l < 256; l++ {
					buf[size-1-0] = byte(l)

					hash := blake2b.Sum256(buf)
					var resultInt uint32
					err := binary.Read(bytes.NewReader(hash[:4]), binary.LittleEndian, &resultInt)
					if err != nil {
						slog.Error("Failed to read hash prefix", "error", err)
						continue
					}

					if resultInt <= threshold {
						return buf[size-SolutionLength:]
					}
				}
			}
		}
	}

	return make([]byte, SolutionLength)
}

func normalizePuzzleBuffer(buf []byte) []byte {
	if len(buf) < PuzzleBytesLength {
		extended := make([]byte, PuzzleBytesLength)
		copy(extended, buf)
		buf = extended
	}

	return buf
}

func (s *Solver) Solve(p *Puzzle) (*Solutions, error) {
	if p.IsZero() {
		return emptySolutions(max(int(p.SolutionsCount), solutionsCount)), nil
	}

	var wg sync.WaitGroup

	buf, err := p.MarshalBinary()
	if err != nil {
		return nil, err
	}

	buf = normalizePuzzleBuffer(buf)

	solutions := make([][]byte, 0)
	size := 0
	var mux sync.Mutex

	threshold := thresholdFromDifficulty(p.Difficulty)
	startTime := time.Now()

	for i := 0; i < int(p.SolutionsCount); i++ {
		wg.Add(1)

		bufCopy := make([]byte, len(buf))
		copy(bufCopy, buf)
		bufCopy[len(buf)-SolutionLength] = byte(i)

		go func(data []byte) {
			defer wg.Done()
			solution := s.solveOne(data, threshold)

			mux.Lock()
			defer mux.Unlock()
			solutions = append(solutions, solution)
			size += len(solution)
		}(bufCopy)
	}

	wg.Wait()

	buffer := make([]byte, size)
	offset := 0
	for _, s := range solutions {
		offset += copy(buffer[offset:], s)
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

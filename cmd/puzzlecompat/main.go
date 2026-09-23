package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

var salt = puzzle.NewSalt([]byte("puzzle-compat-local-salt"))

func generate(ctx context.Context, out io.Writer, count int, difficulty uint8) error {
	w := bufio.NewWriter(out)
	for i := 0; i < count; i++ {
		p := puzzle.NewComputePuzzle(uint64(i+1), [puzzle.PropertyIDSize]byte{1}, difficulty)
		if err := p.Init(puzzle.DefaultValidityPeriod); err != nil {
			return err
		}
		payload, err := p.Serialize(ctx, salt, nil)
		if err != nil {
			return err
		}
		if err := payload.Write(w); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
	}
	return w.Flush()
}

func verify(ctx context.Context, in io.Reader, count int) error {
	scanner := bufio.NewScanner(in)
	verified := 0
	for scanner.Scan() {
		verified++
		if verified > count {
			return fmt.Errorf("received more than %d puzzles", count)
		}
		payload, err := puzzle.ParseVerifyPayload[puzzle.ComputePuzzle](ctx, scanner.Bytes())
		if err != nil {
			return fmt.Errorf("puzzle %d: %w", verified, err)
		}
		if !time.Now().Before(payload.Puzzle().Expiration()) {
			return fmt.Errorf("puzzle %d: expired", verified)
		}
		if err := payload.VerifySignature(ctx, salt, nil); err != nil {
			return fmt.Errorf("puzzle %d: %w", verified, err)
		}
		if _, result := payload.VerifySolutions(ctx); result != puzzle.VerifyNoError {
			return fmt.Errorf("puzzle %d: %s", verified, result)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if verified != count {
		return fmt.Errorf("expected %d puzzles, received %d", count, verified)
	}
	fmt.Fprintf(os.Stderr, "verified %d puzzles\n", verified)
	return nil
}

func main() {
	mode := flag.String("mode", "", "generate or verify")
	count := flag.Int("count", 1000, "number of puzzles to generate or verify")
	difficulty := flag.Int("difficulty", 48, "generated puzzle difficulty (1-255)")
	flag.Parse()

	var err error
	ctx := context.Background()
	switch {
	case *count < 1:
		err = fmt.Errorf("count must be positive")
	case *mode == "generate" && (*difficulty < 1 || *difficulty > 255):
		err = fmt.Errorf("difficulty must be between 1 and 255")
	case *mode == "generate":
		err = generate(ctx, os.Stdout, *count, uint8(*difficulty))
	case *mode == "verify":
		err = verify(ctx, os.Stdin, *count)
	default:
		err = fmt.Errorf("mode must be generate or verify")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

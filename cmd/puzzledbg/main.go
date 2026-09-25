package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	solveFlag = flag.Bool("solve", false, "Solve puzzle instead of printing")
)

func main() {
	flag.Parse()

	common.SetupTraceLogs()

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	responseStr := string(data)
	puzzleStr, _, _ := strings.Cut(responseStr, ".")
	decodedData, err := base64.StdEncoding.DecodeString(puzzleStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error base64-decoding data: %v\n", err)
		os.Exit(2)
	}

	p := new(puzzle.ComputePuzzle)
	err = p.UnmarshalBinary(decodedData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing puzzle: %v\n", err)
		os.Exit(3)
	}

	out, err := puzzleOutput(context.Background(), p, responseStr, *solveFlag)
	if err != nil {
		if *solveFlag {
			fmt.Fprintf(os.Stderr, "Error solving puzzle: %v\n", err)
			os.Exit(5)
		}
		fmt.Fprintf(os.Stderr, "Error marshalling puzzle: %v\n", err)
		os.Exit(4)
	}
	fmt.Print(out)
}

func puzzleOutput(ctx context.Context, p *puzzle.ComputePuzzle, responseStr string, solve bool) (string, error) {
	if solve {
		solutions, err := (&puzzle.ComputeSolver{}).Solve(ctx, p)
		if err != nil {
			return "", err
		}
		return solutions.String() + "." + responseStr, nil
	}

	body, err := p.MarshalBinary()
	if err != nil {
		return "", err
	}
	propertyID := p.PropertyID()
	propertyUUID := pgtype.UUID{Valid: true, Bytes: propertyID}
	challenge := "blake2b"
	if p.Challenge() == puzzle.ChallengeArgon2ID {
		challenge = "argon2id"
	}
	m := map[string]interface{}{
		"Version":        body[0],
		"Challenge":      challenge,
		"PuzzleID":       p.PuzzleID(),
		"PropertyID":     propertyUUID.String(),
		"Difficulty":     p.Difficulty(),
		"SolutionsCount": p.SolutionsCount(),
		"Expiration":     p.Expiration(),
		"IsStub":         p.IsStub(),
		"IsZero":         p.IsZero(),
	}
	out, err := json.MarshalIndent(m, "", "  ")
	return string(out), err
}

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

	if *solveFlag {
		solver := &puzzle.ComputeSolver{}
		solutions, err := solver.Solve(context.Background(), p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error solving puzzle: %v\n", err)
			os.Exit(5)
		}

		fmt.Printf("%s.%s", solutions.String(), responseStr)
	} else {
		m := make(map[string]interface{})
		propertyID := p.PropertyID()
		var propertyUUID pgtype.UUID
		propertyUUID.Valid = true
		copy(propertyUUID.Bytes[:], propertyID[:])

		m["PuzzleID"] = p.PuzzleID()
		m["PropertyID"] = propertyUUID.String()
		m["Difficulty"] = p.Difficulty()
		m["SolutionsCount"] = p.SolutionsCount()
		m["Expiration"] = p.Expiration()
		m["IsStub"] = p.IsStub()
		m["IsZero"] = p.IsZero()

		out, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshalling puzzle: %v\n", err)
			os.Exit(4)
		}

		fmt.Print(string(out))
	}
}

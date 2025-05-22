package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

func main() {
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

	p := new(puzzle.Puzzle)
	err = p.UnmarshalBinary(decodedData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing puzzle: %v\n", err)
		os.Exit(3)
	}

	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshalling puzzle: %v\n", err)
		os.Exit(4)
	}

	fmt.Print(string(out))
}

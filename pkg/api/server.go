package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

const (
	maxSolutionsBodySize = 256 * 1024
)

type Server struct {
	Auth   *AuthMiddleware
	Prefix string
	Salt   []byte
}

func (s *Server) Setup(router *http.ServeMux) {
	if len(s.Prefix) > 0 {
		prefix := s.Prefix
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + s.Prefix
		}

		s.setupWithPrefix(prefix, router)
	} else {
		s.setupWithPrefix("", router)
	}
}

func (s *Server) setupWithPrefix(prefix string, router *http.ServeMux) {
	router.HandleFunc(prefix+common.PuzzleEndpoint, Method(http.MethodGet, s.Auth.Sitekey(s.puzzle)))
	// TODO: Add authentication for submit endpoint
	router.HandleFunc(prefix+common.VerifyEndpoint, Logged(SafeFormPost(s.Auth.APIKey(s.verify), maxSolutionsBodySize)))
}

func (s *Server) puzzleForProperty(property *dbgen.Property) (*puzzle.Puzzle, error) {
	puzzle, err := puzzle.NewPuzzle()
	if err != nil {
		return nil, err
	}

	puzzle.PropertyID = property.ExternalID.Bytes

	return puzzle, nil
}

func (s *Server) puzzle(w http.ResponseWriter, r *http.Request) {
	if (r.Method != http.MethodGet) && (r.Method != http.MethodOptions) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// TODO: Set CORS for the domain, associated with property
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "x-pc-captcha-version, Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctx := r.Context()
	property := ctx.Value(common.PropertyContextKey).(*dbgen.Property)
	puzzle, err := s.puzzleForProperty(property)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create puzzle", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	puzzleBytes, err := puzzle.MarshalBinary()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize puzzle", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hasher := hmac.New(sha1.New, s.Salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", "error", werr)
	}

	hash := hasher.Sum(nil)
	encodedPuzzle := base64.StdEncoding.EncodeToString(puzzleBytes)
	encodedHash := base64.StdEncoding.EncodeToString(hash)
	response := []byte(fmt.Sprintf("%s.%s", encodedPuzzle, encodedHash))

	slog.DebugContext(ctx, "Prepared new puzzle", "propertyID", property.ID)

	w.WriteHeader(http.StatusOK)
	w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
	w.Header().Set(common.HeaderContentLength, strconv.Itoa(len(response)))
	if _, werr := w.Write(response); werr != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle response", "error", werr)
	}
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	data := r.FormValue(common.ParamResponse)
	parts := strings.Split(data, ".")
	if len(parts) != 3 {
		slog.ErrorContext(ctx, "Wrong number of parts", "count", len(parts))
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	solutionsData, puzzleData, signature := parts[0], parts[1], parts[2]

	puzzleBytes, err := base64.StdEncoding.DecodeString(puzzleData)
	if err != nil {
		http.Error(w, "Failed to decode puzzle data", http.StatusBadRequest)
		return
	}

	hasher := hmac.New(sha1.New, s.Salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", "error", werr)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hash := hasher.Sum(nil)

	requestHash, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode hash bytes", "error", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if !bytes.Equal(hash, requestHash) {
		slog.ErrorContext(ctx, "Puzzle hash is not equal", "error", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	var p puzzle.Puzzle
	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", "error", uerr)
		http.Error(w, "Failed to unmarshal Puzzle data", http.StatusBadRequest)
		return
	}

	// TODO: verify puzzle's account & property & Origin etc.

	solutions, err := puzzle.NewSolutions(solutionsData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode solutions bytes", "error", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if len(puzzleBytes) < puzzle.PuzzleBytesLength {
		extendedPuzzleBytes := make([]byte, puzzle.PuzzleBytesLength)
		copy(extendedPuzzleBytes, puzzleBytes)
		puzzleBytes = extendedPuzzleBytes
	}

	// TODO: Cache submitted solutions to protect against replay attacks
	solutionsCount, err := solutions.Verify(ctx, puzzleBytes, p.Difficulty)
	if (err != nil) || (solutionsCount != int(p.SolutionsCount)) {
		slog.ErrorContext(ctx, "Failed to verify solutions", "error", err, "expected", p.SolutionsCount, "actual", solutionsCount)
		http.Error(w, http.StatusText(http.StatusNotAcceptable), http.StatusNotAcceptable)
		return
	}

	slog.Log(ctx, common.LevelTrace, "Verified solutions", "count", solutionsCount)

	w.WriteHeader(http.StatusOK)
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	slog.Error("Inside catchall handler", "path", r.URL.Path, "method", r.Method)

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		slog.Error("Failed to handle the request", "path", r.URL.Path)

		return
	}
}

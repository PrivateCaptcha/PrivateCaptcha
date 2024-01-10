package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

const (
	PageSuccess = "/submit-success.html"
	PageFailure = "/submit-failure.html"
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
	router.HandleFunc(prefix+common.PuzzleEndpoint, Method(http.MethodGet, s.Auth.Authorized(s.puzzle)))
	router.HandleFunc(prefix+common.SubmitEndpoint, Method(http.MethodPost, s.submit))
}

func (s *Server) puzzleForProperty(property *dbgen.Property) (*common.Puzzle, error) {
	puzzle, err := common.NewPuzzle()
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

	// pause for FE spinner debugging
	time.Sleep(time.Duration(500+rand.Intn(1000)) * time.Millisecond)
	slog.DebugContext(ctx, "Prepared new puzzle", "propertyID", property.ID)

	w.WriteHeader(http.StatusOK)
	w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
	w.Header().Set(common.HeaderContentLength, strconv.Itoa(len(response)))
	if _, werr := w.Write(response); werr != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle response", "error", werr)
	}
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := r.FormValue("private-captcha-solution")
	parts := strings.Split(data, ".")
	if len(parts) != 3 {
		slog.ErrorContext(ctx, "Wrong number of parts", "count", len(parts))
		http.Redirect(w, r, PageFailure, http.StatusSeeOther)
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
		http.Redirect(w, r, PageFailure, http.StatusSeeOther)
		return
	}

	hash := hasher.Sum(nil)

	requestHash, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode hash bytes", "error", err)
		http.Redirect(w, r, PageFailure, http.StatusSeeOther)
		return
	}

	if !bytes.Equal(hash, requestHash) {
		slog.ErrorContext(ctx, "Puzzle hash is not equal", "error", err)
		http.Redirect(w, r, PageFailure, http.StatusSeeOther)
		return
	}

	var puzzle common.Puzzle
	if uerr := puzzle.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", "error", uerr)
		http.Error(w, "Failed to unmarshal Puzzle data", http.StatusBadRequest)
		return
	}

	// TODO: verify puzzle's account & property & Origin etc.

	solutions, err := common.NewSolutions(solutionsData)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to decode solutions bytes", "error", err)
		http.Redirect(w, r, PageFailure, http.StatusSeeOther)
		return
	}

	if len(puzzleBytes) < common.PuzzleBytesLength {
		extendedPuzzleBytes := make([]byte, common.PuzzleBytesLength)
		copy(extendedPuzzleBytes, puzzleBytes)
		puzzleBytes = extendedPuzzleBytes
	}

	solutionsCount, err := solutions.Verify(ctx, puzzleBytes, puzzle.Difficulty)
	if (err != nil) || (solutionsCount != int(puzzle.SolutionsCount)) {
		slog.ErrorContext(ctx, "Failed to verify solutions", "error", err, "expected", puzzle.SolutionsCount, "actual", solutionsCount)
		http.Redirect(w, r, PageFailure, http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, PageSuccess, http.StatusSeeOther)
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	slog.Error("Inside catchall handler", "path", r.URL.Path, "method", r.Method)

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		slog.Error("Failed to handle the request", "path", r.URL.Path)

		return
	}
}

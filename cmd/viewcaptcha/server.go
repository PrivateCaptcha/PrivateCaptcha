package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

const (
	greenPage = `<!DOCTYPE html><html><body style="background-color: green;"></body></html>`
	redPage   = `<!DOCTYPE html><html><body style="background-color: red;"></body></html>`
)

type server struct {
	prefix string
	count  int32
	salt   []byte
}

func (s *server) Setup(router *http.ServeMux) {
	prefix := s.prefix
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + s.prefix
	}

	s.setupWithPrefix(prefix, router)
	//router.HandleFunc("/", catchAll)
}

func (s *server) setupWithPrefix(prefix string, router *http.ServeMux) {
	router.HandleFunc(prefix+common.PuzzleEndpoint, s.chaos(s.puzzle))
	router.HandleFunc(http.MethodPost+" "+prefix+"submit", s.submit)
}

// this helps to test backoff
func (s *server) chaos(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nowCount := atomic.AddInt32(&s.count, 1)
		if nowCount%2 == 1 {
			http.Error(w, "chaos", http.StatusInternalServerError)
		} else {
			next.ServeHTTP(w, r)
		}
	}
}

// mostly copy-paste from api/server.go
func (s *server) puzzle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if (r.Method != http.MethodGet) && (r.Method != http.MethodOptions) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	puzzle, err := puzzle.NewPuzzle()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create puzzle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	puzzle.Difficulty = 90

	puzzleBytes, err := puzzle.MarshalBinary()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize puzzle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hasher := hmac.New(sha1.New, s.salt)
	if _, werr := hasher.Write(puzzleBytes); werr != nil {
		slog.ErrorContext(ctx, "Failed to hash puzzle bytes", common.ErrAttr(werr))
	}

	hash := hasher.Sum(nil)
	encodedPuzzle := base64.StdEncoding.EncodeToString(puzzleBytes)
	encodedHash := base64.StdEncoding.EncodeToString(hash)
	response := []byte(fmt.Sprintf("%s.%s", encodedPuzzle, encodedHash))

	w.WriteHeader(http.StatusOK)
	w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
	if _, werr := w.Write(response); werr != nil {
		slog.ErrorContext(ctx, "Failed to write puzzle response", common.ErrAttr(werr))
	}
}

func (s *server) submit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload := r.FormValue("private-captcha-solution")

	solutionsData, puzzleBytes, err := puzzle.ParseSolutions(ctx, payload, s.salt)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse puzzle", common.ErrAttr(err))
		fmt.Fprintln(w, redPage)
		return
	}

	p := new(puzzle.Puzzle)

	if uerr := p.UnmarshalBinary(puzzleBytes); uerr != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal binary puzzle", common.ErrAttr(uerr))
		fmt.Fprintln(w, redPage)
		return
	}

	tnow := time.Now().UTC()
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Puzzle is expired", "expiration", p.Expiration, "now", tnow)
		return
	}

	if serr := puzzle.VerifySolutions(ctx, p, puzzleBytes, solutionsData); serr != puzzle.VerifyNoError {
		fmt.Fprintln(w, redPage)
		return
	}

	fmt.Fprintln(w, greenPage)
}

func catchAll(w http.ResponseWriter, r *http.Request) {
	slog.Error("Inside catchall handler", "path", r.URL.Path, "method", r.Method)

	if r.URL.Path != "/" {
		http.NotFound(w, r)
		slog.Error("Failed to handle the request", "path", r.URL.Path)

		return
	}
}

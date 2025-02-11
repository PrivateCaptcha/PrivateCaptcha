package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/db"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/monitoring"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/puzzle"
)

const (
	greenPage = `<!DOCTYPE html><html><body style="background-color: green;"></body></html>`
	redPage   = `<!DOCTYPE html><html><body style="background-color: red;"></body></html>`
)

var (
	propertySalt = []byte("pepper")
)

type server struct {
	prefix string
	count  int32
	salt   *puzzle.Salt
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
	router.Handle(prefix+common.PuzzleEndpoint, monitoring.Logged(http.HandlerFunc(s.chaos(s.puzzle))))
	router.Handle(prefix+common.EchoPuzzleEndpoint, monitoring.Logged(http.HandlerFunc(s.chaos(s.zeroPuzzle))))
	router.Handle(http.MethodPost+" "+prefix+"submit", monitoring.Logged(http.HandlerFunc(s.submit)))
}

// this helps to test backoff
func (s *server) chaos(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nowCount := atomic.AddInt32(&s.count, 1)
		if nowCount%2 == 1 {
			slog.WarnContext(r.Context(), "Chaos")
			http.Error(w, "chaos", http.StatusInternalServerError)
		} else {
			next.ServeHTTP(w, r)
		}
	}
}

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

	p := puzzle.NewPuzzle(0 /*puzzle ID*/, [16]byte{}, uint8(common.DifficultyLevelMedium))
	if err := p.Init(); err != nil {
		slog.ErrorContext(ctx, "Failed to create puzzle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	s.writePuzzle(ctx, p, propertySalt, w)
}

func (s *server) zeroPuzzle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if (r.Method != http.MethodGet) && (r.Method != http.MethodOptions) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	p := puzzle.NewPuzzle(0, db.TestPropertyUUID.Bytes, uint8(common.DifficultyLevelSmall))

	s.writePuzzle(ctx, p, nil /*extra salt*/, w)
}

// mostly copy-paste from api/server.go
func (s *server) writePuzzle(ctx context.Context, p *puzzle.Puzzle, extraSalt []byte, w http.ResponseWriter) {
	payload, err := p.Serialize(ctx, s.salt, extraSalt)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to serialize puzzle", common.ErrAttr(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set(common.HeaderContentType, common.ContentTypePlain)
	payload.Write(w)
}

func (s *server) submit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	payload := r.FormValue("private-captcha-solution")

	verifyPayload, err := puzzle.ParseVerifyPayload(ctx, payload)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse verify payload", common.ErrAttr(err))
		fmt.Fprintln(w, redPage)
		return
	}

	p := verifyPayload.Puzzle()

	tnow := time.Now().UTC()
	if !tnow.Before(p.Expiration) {
		slog.WarnContext(ctx, "Puzzle is expired", "expiration", p.Expiration, "now", tnow)
		return
	}

	if serr := verifyPayload.VerifySignature(ctx, s.salt, propertySalt); serr != nil {
		fmt.Fprintln(w, redPage)
		return
	}

	if _, verr := verifyPayload.VerifySolutions(ctx); verr != puzzle.VerifyNoError {
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

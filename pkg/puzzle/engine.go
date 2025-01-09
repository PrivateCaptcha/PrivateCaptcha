package puzzle

import (
	"context"
	"net/http"
	"time"
)

type Engine interface {
	Write(ctx context.Context, p *Puzzle, w http.ResponseWriter) error
	Verify(ctx context.Context, payload string, expectedOwner OwnerIDSource, tnow time.Time) (*Puzzle, VerifyError, error)
}

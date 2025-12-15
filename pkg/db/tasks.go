package db

import (
	"context"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type AsyncTaskHandler = func(ctx context.Context, task *dbgen.AsyncTask) ([]byte, error)

type AsyncTasks interface {
	Register(handler string, fn AsyncTaskHandler) bool
	Execute(ctx context.Context, task *dbgen.AsyncTask) error
}

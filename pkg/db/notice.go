package db

import (
	"context"

	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type PropertyNoticeProvider interface {
	Notice(ctx context.Context, property *dbgen.Property) string
}

type emptyNoticeProvider struct{}

var _ PropertyNoticeProvider = (*emptyNoticeProvider)(nil)

func (d *emptyNoticeProvider) Notice(_ context.Context, _ *dbgen.Property) string {
	return ""
}

func NewNoticeProvider() PropertyNoticeProvider {
	return &emptyNoticeProvider{}
}

package db

import (
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type PropertyNoticeProvider interface {
	Notice(property *dbgen.Property) string
}

type emptyNoticeProvider struct{}

var _ PropertyNoticeProvider = (*emptyNoticeProvider)(nil)

func (d *emptyNoticeProvider) Notice(_ *dbgen.Property) string {
	return ""
}

func NewNoticeProvider() PropertyNoticeProvider {
	return &emptyNoticeProvider{}
}

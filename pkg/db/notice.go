package db

import (
	"context"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type PropertyNoticeProvider interface {
	Notice(ctx context.Context, property *dbgen.Property) string
}

type noticeProvider struct {
	notice common.ConfigItem
	value  string
}

var _ PropertyNoticeProvider = (*noticeProvider)(nil)

func (d *noticeProvider) Notice(_ context.Context, _ *dbgen.Property) string {
	return d.value
}

func (d *noticeProvider) Update() {
	if d.notice != nil {
		d.value = d.notice.Value()
	}
}

func NewNoticeProvider(ci common.ConfigItem) *noticeProvider {
	return &noticeProvider{notice: ci}
}

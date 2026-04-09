package db

import (
	"context"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type PropertyNoticeProvider interface {
	Notice(ctx context.Context, property *dbgen.Property) string
}

type NoticeProvider struct {
	notice common.ConfigItem
	value  string
}

var _ PropertyNoticeProvider = (*NoticeProvider)(nil)

func (d *NoticeProvider) Notice(_ context.Context, _ *dbgen.Property) string {
	return d.value
}

func (d *NoticeProvider) Update() {
	if d.notice != nil {
		d.value = d.notice.Value()
	}
}

func NewNoticeProvider(ci common.ConfigItem) *NoticeProvider {
	return &NoticeProvider{notice: ci}
}

package portal

import (
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	dbgen "github.com/PrivateCaptcha/PrivateCaptcha/pkg/db/generated"
)

type defaultNoticeProvider struct{}

var _ common.PropertyNoticeProvider = (*defaultNoticeProvider)(nil)

func (d *defaultNoticeProvider) Notice(_ *dbgen.Property) string {
	return ""
}

func NewDefaultNoticeProvider() common.PropertyNoticeProvider {
	return &defaultNoticeProvider{}
}

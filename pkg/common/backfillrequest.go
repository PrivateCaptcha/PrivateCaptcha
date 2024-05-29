package common

import (
	"strconv"
)

type BackfillRequest struct {
	OrgID      int32
	UserID     int32
	PropertyID int32
}

func (br *BackfillRequest) Key() string {
	return strconv.Itoa(int(br.PropertyID))
}

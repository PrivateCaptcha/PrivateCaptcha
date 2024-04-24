package common

import (
	"fmt"
	"strconv"
)

type BackfillRequest struct {
	OrgID       int32
	UserID      int32
	PropertyID  int32
	Fingerprint TFingerprint
}

func (br *BackfillRequest) Key() string {
	if br.Fingerprint > 0 {
		return fmt.Sprintf("%d/%d", br.PropertyID, br.Fingerprint)
	}

	return strconv.Itoa(int(br.PropertyID))
}

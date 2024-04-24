package common

import "time"

type AccessRecord struct {
	Fingerprint TFingerprint
	UserID      int32
	OrgID       int32
	PropertyID  int32
	Timestamp   time.Time
}

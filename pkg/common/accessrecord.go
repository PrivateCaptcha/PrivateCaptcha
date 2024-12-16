package common

import "time"

type AccessRecord struct {
	Fingerprint TFingerprint
	UserID      int32
	OrgID       int32
	PropertyID  int32
	Timestamp   time.Time
}

type VerifyRecord struct {
	UserID     int32
	OrgID      int32
	PropertyID int32
	PuzzleID   uint64
	Timestamp  time.Time
	Status     int8
}

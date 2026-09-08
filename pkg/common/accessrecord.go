package common

import "time"

type AccessRecord struct {
	Fingerprint  TFingerprint
	UserID       int32
	OrgID        int32
	PropertyID   int32
	RuleID       int32
	Timestamp    time.Time
	PuzzleID     uint64
	ExpiresAt    time.Time
	IPFamily     uint8
	IPPrefix     uint64
	Browser      string
	BrowserMajor uint16
	OS           string
	Device       string
}

type VerifyRecord struct {
	UserID     int32
	OrgID      int32
	PropertyID int32
	PuzzleID   uint64
	Timestamp  time.Time
	ExpiresAt  time.Time
	Status     int8
}

type FormSubmitRecord struct {
	UserID    int32
	OrgID     int32
	FormID    int32
	Timestamp time.Time
	Status    int8
}

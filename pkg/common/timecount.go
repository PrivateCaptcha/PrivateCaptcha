package common

import "time"

type TimePeriod int

const (
	TimePeriodToday TimePeriod = iota
	TimePeriodWeek  TimePeriod = iota
	TimePeriodMonth TimePeriod = iota
	TimePeriodYear  TimePeriod = iota
)

type TimePeriodStat struct {
	Timestamp     time.Time
	RequestsCount int
	VerifiesCount int
}

type TimeCount struct {
	Timestamp time.Time
	Count     uint32
}

type UserTimeCount struct {
	UserID          uint32
	Timestamp       time.Time
	Count           uint64
	Limit           uint64
	PaddleProductID string
}

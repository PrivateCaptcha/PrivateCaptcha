package common

import "time"

type TimePeriod int

const (
	TimePeriodToday TimePeriod = iota
	TimePeriodWeek  TimePeriod = iota
	TimePeriodMonth TimePeriod = iota
	TimePeriodYear  TimePeriod = iota
)

func (tp TimePeriod) String() string {
	switch tp {
	case TimePeriodToday:
		return "today"
	case TimePeriodWeek:
		return "week"
	case TimePeriodMonth:
		return "month"
	case TimePeriodYear:
		return "year"
	default:
		return "unknown"
	}
}

type TimePeriodStat struct {
	Timestamp     time.Time
	RequestsCount int
	VerifiesCount int
}

type TimeCount struct {
	Timestamp time.Time
	Count     uint32
}

type OrgTimeCount struct {
	OrgID     int32
	Timestamp time.Time
	Count     uint32
}

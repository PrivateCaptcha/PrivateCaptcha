package common

import "time"

const OrgStatsOtherPropertyID int32 = -1

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

type OrgPropertyTimePeriodStat struct {
	PropertyID    int32
	Timestamp     time.Time
	RequestsCount int
}

type OrgTimePeriodStats struct {
	PropertyIDs []int32
	Points      []*OrgPropertyTimePeriodStat
}

type FormSubmitStat struct {
	Timestamp    time.Time
	SuccessCount int
	FailureCount int
}

type FailingFormCandidate struct {
	FormID       int32
	FailureCount uint32
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

type UserReportPropertyStat struct {
	PropertyID      int32
	OrgID           int32
	CurrentRequests uint64
	PrevRequests    uint64
}

type UserReportStats struct {
	Properties     []*UserReportPropertyStat
	SecurityEvents []*UserReportSecurityEvent
}

type UserReportSecurityEvent struct {
	PropertyID       int32
	OrgID            int32
	Timestamp        time.Time
	Requests         uint64
	Verifies         uint64
	FailedVerifies   uint64
	FailureQualified bool
}

type UserReportOptions struct {
	TopPropertiesLimit                int
	SecurityEventsLimit               int
	SecurityEventsPerPropertyLimit    int
	SecurityEventRatioThreshold       float64
	SecurityEventMinimumDominantCount uint64
}

type UserReportAccountStats struct {
	CurrentRequests uint64
	PrevRequests    uint64
	CurrentVerifies uint64
	PrevVerifies    uint64
}

type UserReportFormStat struct {
	FormID             int32
	OrgID              int32
	CurrentSubmissions uint64
	PrevSubmissions    uint64
	CurrentErrors      uint64
	PrevErrors         uint64
}

type UserFormsReportStats struct {
	Forms                   []*UserReportFormStat
	TotalCurrentSubmissions uint64
	TotalPrevSubmissions    uint64
	TotalCurrentErrors      uint64
	TotalPrevErrors         uint64
}

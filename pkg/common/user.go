package common

type UserLimitStatus struct {
	// Requests Limit
	Limit                int64
	IsSubscriptionActive bool
}

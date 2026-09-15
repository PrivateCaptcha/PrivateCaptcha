package maintenance

import (
	"testing"
	"time"
)

func TestExpireInternalTrialsLookback(t *testing.T) {
	job := &ExpireInternalTrialsJob{}
	lockDuration := 3 * time.Hour
	maximumExecutionGap := lockDuration + job.Jitter() + job.Interval() + job.Jitter()

	if got := job.lookback(lockDuration); got < maximumExecutionGap {
		t.Fatalf("lookback = %v, want at least %v", got, maximumExecutionGap)
	}
}

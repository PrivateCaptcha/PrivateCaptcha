package maintenance

import (
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

type uniqueJobCooldownTestJob struct {
	common.StubPeriodicJob
	jitter time.Duration
}

func (j *uniqueJobCooldownTestJob) Jitter() time.Duration { return j.jitter }

func TestUniqueJobCooldown(t *testing.T) {
	const (
		lockDuration = time.Hour
		jitter       = 30 * time.Minute
	)
	inner := &uniqueJobCooldownTestJob{
		StubPeriodicJob: common.StubPeriodicJob(t.Name()),
		jitter:          jitter,
	}
	job := &UniquePeriodicJob{Job: inner, LockDuration: lockDuration}

	job.setCooldown(job.LockDuration + job.Job.Jitter())
	remaining := job.cooldownRemaining(t.Context())
	want := lockDuration + jitter
	if remaining < want-time.Second || remaining > want {
		t.Fatalf("cooldown = %v, want approximately %v", remaining, want)
	}

	if remaining := job.cooldownRemaining(withManualPeriodicRun(t.Context())); remaining != 0 {
		t.Fatalf("manual cooldown = %v, want 0", remaining)
	}

	job.setCooldown(0)
	if remaining := job.cooldownRemaining(t.Context()); remaining > 0 {
		t.Fatalf("cleared cooldown = %v, want no delay", remaining)
	}
}

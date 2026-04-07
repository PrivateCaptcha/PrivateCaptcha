//go:build !enterprise

package maintenance

import (
	"context"
	"time"
)

func (j *ScheduleReportsJob) RunOnceAt(ctx context.Context, params any, tnow time.Time) error {
	return nil
}

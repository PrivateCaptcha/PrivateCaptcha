package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
)

const defaultPuzzleStatsPartitionsPerRun = 3

type PuzzleStatsFinalizer interface {
	FinalizeNextPuzzleStats(ctx context.Context, before time.Time) (bool, error)
}

type PuzzleStatsFinalizationJob struct {
	Finalizer     PuzzleStatsFinalizer
	MaxPartitions int
	Now           func() time.Time
}

var _ common.PeriodicJob = (*PuzzleStatsFinalizationJob)(nil)

func (j *PuzzleStatsFinalizationJob) Name() string {
	return "puzzle-stats-finalization"
}

func (j *PuzzleStatsFinalizationJob) NewParams() any {
	return struct{}{}
}

func (j *PuzzleStatsFinalizationJob) Interval() time.Duration {
	return time.Hour
}

func (j *PuzzleStatsFinalizationJob) Timeout() time.Duration {
	return 5 * time.Minute
}

func (j *PuzzleStatsFinalizationJob) Jitter() time.Duration {
	return time.Minute
}

func (j *PuzzleStatsFinalizationJob) Trigger() <-chan struct{} {
	return nil
}

func (j *PuzzleStatsFinalizationJob) RunOnce(ctx context.Context, params any) error {
	started := time.Now()
	maxPartitions := j.MaxPartitions
	if maxPartitions <= 0 {
		maxPartitions = defaultPuzzleStatsPartitionsPerRun
	}
	now := time.Now
	if j.Now != nil {
		now = j.Now
	}
	before := now()
	processedPartitions := 0
	for range maxPartitions {
		if err := ctx.Err(); err != nil {
			slog.ErrorContext(ctx, "Puzzle statistics finalization stopped", "cutoff", before.UTC(), "partitions", processedPartitions, "duration", time.Since(started), common.ErrAttr(err))
			return err
		}
		processed, err := j.Finalizer.FinalizeNextPuzzleStats(ctx, before)
		if err != nil {
			slog.ErrorContext(ctx, "Puzzle statistics finalization failed", "cutoff", before.UTC(), "partitions", processedPartitions, "duration", time.Since(started), common.ErrAttr(err))
			return err
		}
		if !processed {
			slog.InfoContext(ctx, "Puzzle statistics finalization completed", "cutoff", before.UTC(), "partitions", processedPartitions, "duration", time.Since(started))
			return nil
		}
		processedPartitions++
	}
	slog.InfoContext(ctx, "Puzzle statistics finalization completed", "cutoff", before.UTC(), "partitions", processedPartitions, "duration", time.Since(started))
	return nil
}

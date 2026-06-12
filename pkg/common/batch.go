package common

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"time"
)

var (
	errProcessorPanic = errors.New("processor callback panic")
)

type safeProcessor[T any, B any] struct {
	processor func(context.Context, B) error
}

func (sp *safeProcessor[T, B]) Process(ctx context.Context, batch B) (err error) {
	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "Processor callback recovered from panic", "panic", rvr, "stack", string(debug.Stack()))
			err = errProcessorPanic
		}
	}()

	return sp.processor(ctx, batch)
}

func ProcessBatchArray[T any](ctx context.Context, channel <-chan T, delay time.Duration, triggerSize, maxBatchSize int, processor func(context.Context, []T) error) {
	var batch []T
	sp := &safeProcessor[T, []T]{processor: processor}
	slog.DebugContext(ctx, "Processing batch", "interval", delay.String())

	for running := true; running; {
		if len(batch) > maxBatchSize {
			slog.ErrorContext(ctx, "Dropping pending batch due to errors", "count", len(batch))
			batch = []T{}
		}

		select {
		case <-ctx.Done():
			running = false

		case item, ok := <-channel:
			if !ok {
				running = false
				break
			}

			batch = append(batch, item)

			if len(batch) >= triggerSize {
				slog.Log(ctx, LevelTrace, "Processing batch", "count", len(batch), "reason", "batch")
				if err := sp.Process(ctx, batch); err == nil {
					batch = []T{}
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				slog.Log(ctx, LevelTrace, "Processing batch", "count", len(batch), "reason", "timeout")
				if err := sp.Process(ctx, batch); err == nil {
					batch = []T{}
				}
			}
		}
	}

	if len(batch) > 0 {
		flushCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		slog.InfoContext(ctx, "Processing remaining batch items before shutdown", "count", len(batch))
		if err := sp.Process(flushCtx, batch); err != nil {
			slog.ErrorContext(ctx, "Failed to process remaining batch", "count", len(batch), ErrAttr(err))
		}
	}

	slog.InfoContext(ctx, "Finished processing batch")
}

// as they say, a little copy-paste is better than a little dependency
// it is assumed to be called with such parameters that make uint enough for counting
func ProcessBatchMap[T comparable](ctx context.Context, channel <-chan T, delay time.Duration, triggerSize, maxBatchSize int, processor func(context.Context, map[T]uint) error) {
	defer func() {
		if rvr := recover(); rvr != nil {
			slog.ErrorContext(ctx, "ProcessBatchMap crashed", "panic", rvr, "stack", string(debug.Stack()))
		}
	}()

	sp := &safeProcessor[T, map[T]uint]{processor: processor}
	batch := make(map[T]uint, triggerSize)
	slog.DebugContext(ctx, "Processing batch", "interval", delay.String())

	for running := true; running; {
		if len(batch) > maxBatchSize {
			slog.ErrorContext(ctx, "Dropping pending batch due to errors", "count", len(batch))
			batch = make(map[T]uint, triggerSize)
		}

		select {
		case <-ctx.Done():
			running = false

		case item, ok := <-channel:
			if !ok {
				running = false
				break
			}

			batch[item]++

			if len(batch) >= triggerSize {
				slog.Log(ctx, LevelTrace, "Processing batch", "count", len(batch), "reason", "batch")
				if err := sp.Process(ctx, batch); err == nil {
					batch = make(map[T]uint, triggerSize)
				}
			}
		case <-time.After(delay):
			if len(batch) > 0 {
				slog.Log(ctx, LevelTrace, "Processing batch", "count", len(batch), "reason", "timeout")
				if err := sp.Process(ctx, batch); err == nil {
					batch = make(map[T]uint, triggerSize)
				}
			}
		}
	}

	if len(batch) > 0 {
		flushCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		slog.InfoContext(ctx, "Processing remaining batch items before shutdown", "count", len(batch))
		if err := sp.Process(flushCtx, batch); err != nil {
			slog.ErrorContext(ctx, "Failed to process remaining batch", "count", len(batch), ErrAttr(err))
		}
	}

	slog.InfoContext(ctx, "Finished processing batch")
}

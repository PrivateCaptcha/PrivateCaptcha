package common

import (
	"context"
	"errors"
	"testing"
)

func TestSafeProcessorRecoverFromPanic(t *testing.T) {
	t.Parallel()

	processor := func(ctx context.Context, batch []int) error {
		panic("test panic")
	}

	sp := &safeProcessor[int, []int]{processor: processor}

	ctx := context.Background()
	err := sp.Process(ctx, []int{1, 2, 3})

	if err != errProcessorPanic {
		t.Errorf("Expected errProcessorPanic, got %v", err)
	}
}

func TestSafeProcessorNoError(t *testing.T) {
	t.Parallel()

	processor := func(ctx context.Context, batch []int) error {
		return nil
	}

	sp := &safeProcessor[int, []int]{processor: processor}

	ctx := context.Background()
	err := sp.Process(ctx, []int{1, 2, 3})

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestSafeProcessorWithError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("test error")
	processor := func(ctx context.Context, batch []int) error {
		return expectedErr
	}

	sp := &safeProcessor[int, []int]{processor: processor}

	ctx := context.Background()
	err := sp.Process(ctx, []int{1, 2, 3})

	if err != expectedErr {
		t.Errorf("Expected %v, got %v", expectedErr, err)
	}
}

func TestSafeProcessorWithMapBatch(t *testing.T) {
	t.Parallel()

	processor := func(ctx context.Context, batch map[string]uint) error {
		if len(batch) != 2 {
			return errors.New("unexpected batch size")
		}
		return nil
	}

	sp := &safeProcessor[string, map[string]uint]{processor: processor}

	ctx := context.Background()
	batch := map[string]uint{"a": 1, "b": 2}
	err := sp.Process(ctx, batch)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestSafeProcessorMapPanic(t *testing.T) {
	t.Parallel()

	processor := func(ctx context.Context, batch map[string]uint) error {
		panic("map processor panic")
	}

	sp := &safeProcessor[string, map[string]uint]{processor: processor}

	ctx := context.Background()
	err := sp.Process(ctx, map[string]uint{"a": 1})

	if err != errProcessorPanic {
		t.Errorf("Expected errProcessorPanic, got %v", err)
	}
}

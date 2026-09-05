package common

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
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

func TestProcessBatchArrayChannelCloseLosesData(t *testing.T) {
	var mu sync.Mutex
	var processedItems []int

	processor := func(ctx context.Context, batch []int) error {
		mu.Lock()
		defer mu.Unlock()
		processedItems = append(processedItems, batch...)
		return nil
	}

	ch := make(chan int, 100)
	ctx := context.Background()

	go ProcessBatchArray(ctx, ch, 10*time.Second, 100, 1000, processor)
	time.Sleep(50 * time.Millisecond)

	for i := 1; i <= 50; i++ {
		ch <- i
	}
	time.Sleep(100 * time.Millisecond)

	close(ch) // <-- Channel close instead of context cancel
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(processedItems)
	mu.Unlock()

	if count != 50 {
		t.Errorf("Expected 50 items, got %d. Lost: %d", count, 50-count)
	}
}

func TestProcessBatchMapChannelCloseLosesData(t *testing.T) {
	var mu sync.Mutex
	var processedItems []int

	processor := func(ctx context.Context, batch map[int]uint) error {
		mu.Lock()
		defer mu.Unlock()
		for b := range batch {
			processedItems = append(processedItems, b)
		}
		return nil
	}

	ch := make(chan int, 100)
	ctx := context.Background()

	go ProcessBatchMap(ctx, ch, 10*time.Second, 100, 1000, processor)
	time.Sleep(50 * time.Millisecond)

	for i := 1; i <= 50; i++ {
		ch <- i
	}
	time.Sleep(100 * time.Millisecond)

	close(ch) // <-- Channel close instead of context cancel
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(processedItems)
	mu.Unlock()

	if count != 50 {
		t.Errorf("Expected 50 items, got %d. Lost: %d", count, 50-count)
	}
}

func TestProcessBatchMapTriggersAfterUpdatesToSameKey(t *testing.T) {
	const triggerSize = 3

	ctx, cancel := context.WithCancel(context.Background())
	processed := make(chan map[string]uint, 1)
	done := make(chan struct{})
	processor := func(ctx context.Context, batch map[string]uint) error {
		processed <- batch
		return nil
	}

	ch := make(chan string, triggerSize)
	go func() {
		defer close(done)
		ProcessBatchMap(ctx, ch, time.Hour, triggerSize, 100, processor)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	for range triggerSize {
		ch <- "key"
	}

	select {
	case batch := <-processed:
		if len(batch) != 1 || batch["key"] != triggerSize {
			t.Fatalf("Expected 3 updates to one key, got %v", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("Expected batch to be processed after reaching the update trigger size")
	}
}

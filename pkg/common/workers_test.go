package common

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestWaitWorkersDoesNotStopWorkers(t *testing.T) {
	var workers sync.WaitGroup
	workers.Add(1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := WaitWorkers(ctx, &workers); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait result: %v", err)
	}
	workers.Done()
	if err := WaitWorkers(t.Context(), &workers); err != nil {
		t.Fatal(err)
	}
}

package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionOperationGateSerializesSameSession(t *testing.T) {
	gate := newSessionOperationGate()
	firstRelease, err := gate.acquireSession(t.Context(), "shared-sid")
	if err != nil {
		t.Fatal(err)
	}

	secondStarted := make(chan struct{})
	secondAcquired := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		release, err := gate.acquireSession(t.Context(), "shared-sid")
		if err == nil {
			close(secondAcquired)
			release()
		}
		secondDone <- err
	}()
	<-secondStarted

	select {
	case <-secondAcquired:
		t.Fatal("second operation acquired the same SID before release")
	case <-time.After(20 * time.Millisecond):
	}
	firstRelease()
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if size := gate.operations.EstimatedSize(); size != 0 {
		t.Fatalf("operation cache size = %d, want 0", size)
	}
}

func TestSessionOperationGateBatchBlocksOnlyIncludedSessions(t *testing.T) {
	gate := newSessionOperationGate()
	batchRelease, err := gate.acquireBatch(t.Context(), []string{"batched-sid"})
	if err != nil {
		t.Fatal(err)
	}
	defer batchRelease()

	unrelatedRelease, err := gate.acquireSession(t.Context(), "unrelated-sid")
	if err != nil {
		t.Fatal(err)
	}
	unrelatedRelease()

	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := gate.acquireSession(waitCtx, "batched-sid"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("included SID acquire error = %v, want deadline exceeded", err)
	}
}

func TestSessionOperationGateAllowsDisjointBatches(t *testing.T) {
	gate := newSessionOperationGate()
	firstRelease, err := gate.acquireBatch(t.Context(), []string{"first-sid"})
	if err != nil {
		t.Fatal(err)
	}
	defer firstRelease()

	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	secondRelease, err := gate.acquireBatch(waitCtx, []string{"second-sid"})
	if err != nil {
		t.Fatalf("disjoint batch acquire error = %v", err)
	}
	secondRelease()
}

func TestSessionOperationGateDeduplicatesBatchSessions(t *testing.T) {
	gate := newSessionOperationGate()
	waitCtx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	release, err := gate.acquireBatch(waitCtx, []string{"second-sid", "first-sid", "second-sid"})
	if err != nil {
		t.Fatalf("batch acquire error = %v", err)
	}
	release()
}

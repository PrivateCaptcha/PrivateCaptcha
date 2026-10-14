package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/common"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/config"
	"github.com/PrivateCaptcha/PrivateCaptcha/pkg/maintenance"
)

func TestShutdownReadinessPropagation(t *testing.T) {
	health := &maintenance.HealthCheckJob{
		MaintenanceMode: config.NewStaticValue(common.MaintenanceModeKey, "false"),
	}
	probe := httptest.NewServer(http.HandlerFunc(health.ReadyHandler))
	defer probe.Close()
	quit := make(chan bool, 1)
	const delay = 100 * time.Millisecond
	started := time.Now()
	go notifyShutdown(t.Context(), health, delay, func(immediate bool) { quit <- immediate }, false)
	for !health.IsShuttingDown() {
		if time.Since(started) > time.Second {
			t.Fatal("readiness was not marked unavailable")
		}
		runtime.Gosched()
	}
	resp, err := probe.Client().Get(probe.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness returned %d", resp.StatusCode)
	}
	if <-quit {
		t.Fatal("normal shutdown became immediate")
	}
	if elapsed := time.Since(started); elapsed < delay {
		t.Fatalf("quit notified before readiness delay: %v", elapsed)
	}
}

func TestShutdownReadinessZeroAndImmediate(t *testing.T) {
	for _, immediate := range []bool{false, true} {
		health := &maintenance.HealthCheckJob{}
		quit := make(chan bool, 1)
		delay := time.Duration(0)
		if immediate {
			delay = time.Hour
		}
		go notifyShutdown(t.Context(), health, delay, func(immediate bool) { quit <- immediate }, immediate)
		select {
		case got := <-quit:
			if got != immediate || !health.IsShuttingDown() {
				t.Fatalf("shutdown notification: %v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("shutdown waited despite zero delay or immediate termination")
		}
	}
}

func TestImmediateShutdownPreemptsReadinessPropagation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	health := &maintenance.HealthCheckJob{}
	quit := make(chan bool, 2)
	var once sync.Once
	notify := func(immediate bool) {
		once.Do(func() { quit <- immediate })
	}
	normalDone := make(chan struct{})
	go func() {
		notifyShutdown(ctx, health, time.Hour, notify, false)
		close(normalDone)
	}()
	deadline := time.Now().Add(time.Second)
	for !health.IsShuttingDown() {
		if time.Now().After(deadline) {
			t.Fatal("normal shutdown did not begin readiness draining")
		}
		runtime.Gosched()
	}
	immediateDone := make(chan struct{})
	go func() {
		notifyShutdown(ctx, health, time.Hour, notify, true)
		close(immediateDone)
	}()
	select {
	case immediate := <-quit:
		if !immediate {
			t.Fatal("normal shutdown notification won during readiness draining")
		}
	case <-time.After(time.Second):
		t.Fatal("immediate shutdown waited for readiness propagation")
	}
	cancel()
	for _, done := range []<-chan struct{}{normalDone, immediateDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("shutdown request did not finish")
		}
	}
	notifyShutdown(ctx, health, time.Hour, notify, true)
	select {
	case immediate := <-quit:
		t.Fatalf("duplicate shutdown notification: %v", immediate)
	default:
	}
}

func TestShutdownWorkersAndCachesRunInParallel(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	workerStarted := make(chan struct{})
	cacheStarted := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		close(workerStarted)
		<-cacheStarted
	})
	err := finishShutdown(ctx, &workers, func(ctx context.Context) error {
		close(cacheStarted)
		select {
		case <-workerStarted:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil || ctx.Err() != nil {
		t.Fatalf("shutdown did not complete in parallel: %v", err)
	}
}

func TestShutdownCompletionUsesSharedDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	workerStarted := make(chan struct{})
	cacheStarted := make(chan struct{})
	done := make(chan error, 1)
	var workers sync.WaitGroup
	workers.Go(func() {
		close(workerStarted)
		<-ctx.Done()
	})
	go func() {
		done <- finishShutdown(ctx, &workers, func(pctx context.Context) error {
			if pctx != ctx {
				t.Error("cache persistence received a different context")
			}
			close(cacheStarted)
			<-pctx.Done()
			return pctx.Err()
		})
	}()
	<-workerStarted
	<-cacheStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker wait result: %v", err)
	}
}

// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_BasicExecution(t *testing.T) {
	s := newScheduler()
	defer s.stop()

	var executed atomic.Bool
	done := make(chan struct{})

	s.schedule("ws", "pkg", "uri", func(ctx context.Context) {
		executed.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not execute in time")
	}

	if !executed.Load() {
		t.Error("job was not executed")
	}
}

func TestScheduler_Coalescing(t *testing.T) {
	s := newScheduler()
	defer s.stop()

	// Block the worker so we can queue multiple jobs.
	blocker := make(chan struct{})
	s.schedule("ws", "pkg", "uri-blocker", func(ctx context.Context) {
		<-blocker
	})

	var count atomic.Int64
	done := make(chan struct{})

	// Schedule multiple jobs for the same key. Only the last should run.
	for i := 0; i < 5; i++ {
		s.schedule("ws", "pkg", "uri", func(ctx context.Context) {
			count.Add(1)
			select {
			case <-done:
			default:
				close(done)
			}
		})
	}

	// Unblock the worker.
	close(blocker)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("coalesced job did not execute")
	}

	// Give a small window for any extra executions.
	time.Sleep(100 * time.Millisecond)

	got := count.Load()
	if got != 1 {
		t.Errorf("expected 1 execution after coalescing, got %d", got)
	}
}

func TestScheduler_Stop(t *testing.T) {
	s := newScheduler()

	var executed atomic.Bool
	s.schedule("ws", "pkg", "uri", func(ctx context.Context) {
		executed.Store(true)
	})

	// Give it a moment to process.
	time.Sleep(100 * time.Millisecond)

	s.stop()
	// Stop should complete without deadlocking.
}

func TestScheduler_ConcurrentSchedule(t *testing.T) {
	s := newScheduler()
	defer s.stop()

	var wg sync.WaitGroup
	var count atomic.Int64

	// Schedule many concurrent jobs on different keys.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		key := string(rune('a' + i%10))
		go func() {
			defer wg.Done()
			s.schedule("ws", "pkg", key, func(ctx context.Context) {
				count.Add(1)
			})
		}()
	}

	wg.Wait()

	// Wait for jobs to drain.
	time.Sleep(500 * time.Millisecond)

	got := count.Load()
	if got == 0 {
		t.Error("expected some jobs to execute")
	}
}

func TestScheduler_Backpressure(t *testing.T) {
	s := newScheduler()
	defer s.stop()

	// Block the worker so the queue fills up.
	blocker := make(chan struct{})
	s.schedule("ws", "pkg", "blocker", func(ctx context.Context) {
		<-blocker
	})

	// Fill the queue beyond capacity. Each needs a unique key to avoid
	// coalescing (which replaces in-place without enqueuing).
	for i := 0; i < defaultMaxQueueSize+10; i++ {
		s.schedule("ws", "pkg", fmt.Sprintf("key-%d", i), func(ctx context.Context) {})
	}

	// Unblock and let everything drain.
	close(blocker)

	// Should complete without deadlocking or panicking.
}

func TestScheduler_PanicRecovery(t *testing.T) {
	s := newScheduler()
	defer s.stop()

	done := make(chan struct{})

	// Schedule a panicking job.
	s.schedule("ws", "pkg", "panic-uri", func(ctx context.Context) {
		panic("test panic")
	})

	// Schedule a normal job after the panic. It should still execute.
	s.schedule("ws", "pkg", "normal-uri", func(ctx context.Context) {
		close(done)
	})

	select {
	case <-done:
		// Success: the scheduler recovered from panic and continued.
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not recover from panic")
	}
}

func TestScheduler_CancelWorkspaceRoot(t *testing.T) {
	s := newScheduler()
	defer s.stop()

	runningCancelled := make(chan struct{})
	var ranRootA atomic.Int64
	var ranRootB atomic.Int64

	// Running job in root A waits until cancelled.
	s.schedule("/rootA", "pkg", "running", func(ctx context.Context) {
		select {
		case <-ctx.Done():
			close(runningCancelled)
		case <-time.After(2 * time.Second):
			t.Error("running rootA job was not cancelled in time")
		}
	})

	// Let the running job start and occupy the worker.
	time.Sleep(50 * time.Millisecond)

	// Pending jobs in root A and root B.
	s.schedule("/rootA", "pkg", "pending", func(ctx context.Context) {
		ranRootA.Add(1)
	})
	s.schedule("/rootB", "pkg", "pending", func(ctx context.Context) {
		ranRootB.Add(1)
	})

	s.cancelWorkspaceRoot("/rootA")

	select {
	case <-runningCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("expected running rootA job to be cancelled")
	}

	// Give the worker a chance to execute remaining jobs.
	time.Sleep(200 * time.Millisecond)

	if ranRootA.Load() != 0 {
		t.Fatalf("expected rootA pending jobs to be cancelled, got %d runs", ranRootA.Load())
	}
	if ranRootB.Load() != 1 {
		t.Fatalf("expected rootB pending job to run once, got %d", ranRootB.Load())
	}
}

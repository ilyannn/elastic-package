// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package lsp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

const defaultMaxQueueSize = 64

// jobFunc is the callback for a scheduled validation job.
type jobFunc func(ctx context.Context)

// scheduler implements a latest-wins coalescing queue keyed by
// (workspaceRoot, packageRoot, uri). When a new job arrives for a key
// that already has a pending job, the old job is superseded.
type scheduler struct {
	mu       sync.Mutex
	pending  map[string]*scheduledJob // key -> job
	queue    chan string              // bounded queue of keys
	maxQueue int
	version  atomic.Int64
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

type scheduledJob struct {
	key     string
	version int64
	fn      jobFunc
	ctx     context.Context
	cancel  context.CancelFunc
}

func newScheduler() *scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	s := &scheduler{
		pending:  make(map[string]*scheduledJob),
		queue:    make(chan string, defaultMaxQueueSize),
		maxQueue: defaultMaxQueueSize,
		ctx:      ctx,
		cancel:   cancel,
	}
	s.wg.Add(1)
	go s.worker()
	return s
}

// schedule enqueues a job. If a job with the same key is already pending, it
// is superseded (its context is cancelled).
func (s *scheduler) schedule(workspaceRoot, packageRoot, uri string, fn jobFunc) {
	key := workspaceRoot + "\x00" + packageRoot + "\x00" + uri
	ver := s.version.Add(1)

	jobCtx, jobCancel := context.WithCancel(s.ctx)
	job := &scheduledJob{
		key:     key,
		version: ver,
		fn:      fn,
		ctx:     jobCtx,
		cancel:  jobCancel,
	}

	s.mu.Lock()
	if old, ok := s.pending[key]; ok {
		old.cancel()
		logDebug("scheduler", map[string]interface{}{
			"event":   "coalesced",
			"key":     key,
			"old_ver": old.version,
			"new_ver": ver,
		})
		// Replace in-place; the key is already in the queue.
		s.pending[key] = job
		s.mu.Unlock()
		return
	}
	s.pending[key] = job
	s.mu.Unlock()

	// Try to enqueue; if queue is full apply backpressure by dropping.
	select {
	case s.queue <- key:
		logDebug("scheduler", map[string]interface{}{
			"event":   "queued",
			"key":     key,
			"version": ver,
		})
	default:
		logWarn("scheduler", map[string]interface{}{
			"event":   "queue-backpressure",
			"key":     key,
			"version": ver,
		})
		// Drop this enqueue attempt and cancel the job.
		jobCancel()
		s.mu.Lock()
		if cur, ok := s.pending[key]; ok && cur.version == ver {
			delete(s.pending, key)
		}
		s.mu.Unlock()
	}
}

func (s *scheduler) worker() {
	defer s.wg.Done()
	for {
		select {
		case key := <-s.queue:
			s.mu.Lock()
			job, ok := s.pending[key]
			if ok {
				delete(s.pending, key)
			}
			s.mu.Unlock()

			if !ok {
				continue
			}

			s.executeJob(job)

		case <-s.ctx.Done():
			return
		}
	}
}

func (s *scheduler) executeJob(job *scheduledJob) {
	defer func() {
		if r := recover(); r != nil {
			logError("scheduler", map[string]interface{}{
				"event":   "panic-recovered",
				"key":     job.key,
				"version": job.version,
				"panic":   fmt.Sprintf("%v", r),
			})
		}
	}()

	defer job.cancel()

	// Check if job was already cancelled (superseded).
	select {
	case <-job.ctx.Done():
		logDebug("scheduler", map[string]interface{}{
			"event":   "cancelled",
			"key":     job.key,
			"version": job.version,
		})
		return
	default:
	}

	logDebug("scheduler", map[string]interface{}{
		"event":   "started",
		"key":     job.key,
		"version": job.version,
	})

	job.fn(job.ctx)

	logDebug("scheduler", map[string]interface{}{
		"event":   "finished",
		"key":     job.key,
		"version": job.version,
	})
}

func (s *scheduler) stop() {
	s.cancel()
	s.wg.Wait()
}

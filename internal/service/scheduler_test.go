package service

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_Basic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 2,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Create a simple job
	executed := false
	job := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
		executed = true
		return &AlgoResult{ID: "result1"}, nil
	})

	// Submit job
	err := scheduler.Submit(job)
	if err != nil {
		t.Fatalf("failed to submit job: %v", err)
	}

	// Wait for job to complete
	err = job.Wait(context.Background())
	if err != nil {
		t.Fatalf("job wait failed: %v", err)
	}

	// Verify execution
	if !executed {
		t.Error("job was not executed")
	}

	if job.State != JobStateSucceeded {
		t.Errorf("expected job state SUCCEEDED, got %s", job.State.String())
	}

	if job.ResultID != "result1" {
		t.Errorf("expected result ID 'result1', got %s", job.ResultID)
	}
}

func TestScheduler_ParallelLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 2,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	var maxConcurrent int32
	var currentConcurrent int32
	var wg sync.WaitGroup

	// Submit 5 jobs that track concurrency
	for i := 0; i < 5; i++ {
		wg.Add(1)
		job := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
			defer wg.Done()
			current := atomic.AddInt32(&currentConcurrent, 1)

			// Track max concurrent
			for {
				max := atomic.LoadInt32(&maxConcurrent)
				if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&currentConcurrent, -1)
			return &AlgoResult{ID: "result"}, nil
		})

		if err := scheduler.Submit(job); err != nil {
			t.Fatalf("failed to submit job: %v", err)
		}
	}

	// Wait for all jobs
	wg.Wait()

	// Verify max concurrent was respected
	if maxConcurrent > int32(config.MaxParallelJobs) {
		t.Errorf("exceeded max parallel jobs: got %d, max %d", maxConcurrent, config.MaxParallelJobs)
	}
}

func TestScheduler_QueueFull(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 1,
		MaxQueueSize:    2,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Block the scheduler with a long job
	blockCh := make(chan struct{})
	blockJob := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
		<-blockCh
		return &AlgoResult{ID: "block"}, nil
	})
	scheduler.Submit(blockJob)

	// Wait for blockJob to start
	time.Sleep(50 * time.Millisecond)

	// Fill the queue
	for i := 0; i < config.MaxQueueSize; i++ {
		job := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
			return &AlgoResult{ID: "queued"}, nil
		})
		if err := scheduler.Submit(job); err != nil {
			t.Fatalf("failed to submit job %d: %v", i, err)
		}
	}

	// Next job should fail
	overflowJob := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
		return &AlgoResult{ID: "overflow"}, nil
	})
	err := scheduler.Submit(overflowJob)
	if err == nil {
		t.Error("expected error when queue is full")
	}

	// Unblock
	close(blockCh)
}

func TestScheduler_Priority(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 1,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Block the scheduler
	blockCh := make(chan struct{})
	blockJob := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
		<-blockCh
		return &AlgoResult{ID: "block"}, nil
	})
	scheduler.Submit(blockJob)
	time.Sleep(50 * time.Millisecond)

	// Submit jobs with different priorities
	var executionOrder []int
	var orderMu sync.Mutex
	var wg sync.WaitGroup

	jobs := make([]*Job, 3)
	for i := 0; i < 3; i++ {
		priority := int32(i) // 0, 1, 2 (higher is more urgent)
		idx := i
		wg.Add(1)
		jobs[i] = NewJob("tenant1", AlgoKindShortestPath, priority, func(ctx context.Context) (*AlgoResult, error) {
			defer wg.Done()
			orderMu.Lock()
			executionOrder = append(executionOrder, idx)
			orderMu.Unlock()
			return &AlgoResult{}, nil
		})
		scheduler.Submit(jobs[i])
	}

	// Unblock and let jobs run
	close(blockCh)

	// Wait for all jobs to complete
	wg.Wait()

	// Higher priority should run first
	orderMu.Lock()
	defer orderMu.Unlock()

	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 jobs executed, got %d", len(executionOrder))
	}

	// Priority 2 should be first, then 1, then 0
	if executionOrder[0] != 2 {
		t.Errorf("expected highest priority (2) first, got %d", executionOrder[0])
	}
}

func TestScheduler_CancelJob(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 1,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Create a job that waits for context cancellation
	job := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})

	scheduler.Submit(job)
	time.Sleep(50 * time.Millisecond)

	// Cancel the job
	scheduler.CancelJob(job.ID)

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Read state under lock to avoid race condition
	job.mu.Lock()
	state := job.State
	job.mu.Unlock()

	if state != JobStateCanceled && state != JobStateFailed {
		t.Errorf("expected job state CANCELED or FAILED, got %s", state.String())
	}
}

func TestScheduler_TenantJobCount(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 5,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Create jobs for two tenants
	blockCh := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		job := NewJob("tenant1", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
			defer wg.Done()
			<-blockCh
			return &AlgoResult{}, nil
		})
		scheduler.Submit(job)
	}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		job := NewJob("tenant2", AlgoKindShortestPath, 0, func(ctx context.Context) (*AlgoResult, error) {
			defer wg.Done()
			<-blockCh
			return &AlgoResult{}, nil
		})
		scheduler.Submit(job)
	}

	// Wait for jobs to start
	time.Sleep(100 * time.Millisecond)

	// Check tenant counts
	tenant1Count := scheduler.TenantJobCount("tenant1")
	tenant2Count := scheduler.TenantJobCount("tenant2")

	if tenant1Count != 3 {
		t.Errorf("expected tenant1 count 3, got %d", tenant1Count)
	}
	if tenant2Count != 2 {
		t.Errorf("expected tenant2 count 2, got %d", tenant2Count)
	}

	// Unblock and cleanup
	close(blockCh)
	wg.Wait()
}

func TestScheduler_RunSync(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 2,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Run synchronously
	result, err := scheduler.RunSync(context.Background(), func(ctx context.Context) (*AlgoResult, error) {
		return &AlgoResult{ID: "sync-result"}, nil
	})

	if err != nil {
		t.Fatalf("RunSync failed: %v", err)
	}

	if result.ID != "sync-result" {
		t.Errorf("expected result ID 'sync-result', got %s", result.ID)
	}
}

func TestScheduler_Acquire(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 1,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Acquire a slot
	release, err := scheduler.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// TryAcquire should fail now
	release2 := scheduler.TryAcquire()
	if release2 != nil {
		t.Error("TryAcquire should fail when at capacity")
		release2()
	}

	// Release and try again
	release()

	release3 := scheduler.TryAcquire()
	if release3 == nil {
		t.Error("TryAcquire should succeed after release")
	} else {
		release3()
	}
}

func TestScheduler_Stats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := SchedulerConfig{
		MaxParallelJobs: 2,
		MaxQueueSize:    10,
		JobTimeout:      5 * time.Second,
	}

	scheduler := NewScheduler(config, logger)
	defer scheduler.Shutdown(context.Background())

	// Check initial stats
	stats := scheduler.Stats()
	if stats.MaxParallelJobs != 2 {
		t.Errorf("expected MaxParallelJobs 2, got %d", stats.MaxParallelJobs)
	}
	if stats.MaxQueueSize != 10 {
		t.Errorf("expected MaxQueueSize 10, got %d", stats.MaxQueueSize)
	}
	if stats.ActiveJobs != 0 {
		t.Errorf("expected ActiveJobs 0, got %d", stats.ActiveJobs)
	}
}

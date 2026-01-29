// Package service provides core services for the graph-engine.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/naisa-ai/graph-engine/internal/metrics"
)

// JobState represents the state of a job.
type JobState int

const (
	JobStatePending JobState = iota
	JobStateRunning
	JobStateSucceeded
	JobStateFailed
	JobStateCanceled
)

// String returns the string representation of JobState.
func (s JobState) String() string {
	switch s {
	case JobStatePending:
		return "PENDING"
	case JobStateRunning:
		return "RUNNING"
	case JobStateSucceeded:
		return "SUCCEEDED"
	case JobStateFailed:
		return "FAILED"
	case JobStateCanceled:
		return "CANCELED"
	default:
		return "UNKNOWN"
	}
}

// Job represents a scheduled algorithm job.
type Job struct {
	ID        string
	TenantID  string
	AlgoKind  AlgoKind
	Priority  int32
	State     JobState
	CreatedAt time.Time
	StartedAt time.Time
	EndedAt   time.Time
	ResultID  string
	Error     error

	// Execution function
	execFn func(ctx context.Context) (*AlgoResult, error)

	// Cancellation
	cancel context.CancelFunc
	ctx    context.Context

	// Synchronization
	done chan struct{}
	mu   sync.Mutex
}

// NewJob creates a new job.
func NewJob(tenantID string, algoKind AlgoKind, priority int32, execFn func(ctx context.Context) (*AlgoResult, error)) *Job {
	return &Job{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		AlgoKind:  algoKind,
		Priority:  priority,
		State:     JobStatePending,
		CreatedAt: time.Now(),
		execFn:    execFn,
		done:      make(chan struct{}),
	}
}

// Wait blocks until the job completes.
func (j *Job) Wait(ctx context.Context) error {
	select {
	case <-j.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Cancel cancels the job.
func (j *Job) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.State == JobStatePending || j.State == JobStateRunning {
		j.State = JobStateCanceled
		j.EndedAt = time.Now()
		if j.cancel != nil {
			j.cancel()
		}
		select {
		case <-j.done:
		default:
			close(j.done)
		}
	}
}

// SchedulerConfig holds configuration for the scheduler.
type SchedulerConfig struct {
	MaxParallelJobs int
	MaxQueueSize    int
	JobTimeout      time.Duration
}

// DefaultSchedulerConfig returns default scheduler configuration.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		MaxParallelJobs: 4,
		MaxQueueSize:    100,
		JobTimeout:      5 * time.Minute,
	}
}

// Scheduler manages job execution with concurrency limits.
type Scheduler struct {
	config SchedulerConfig
	logger *slog.Logger

	// Semaphore for limiting parallel jobs
	sem chan struct{}

	// Job tracking
	jobs   map[string]*Job
	jobsMu sync.RWMutex

	// Queue for pending jobs
	queue   []*Job
	queueMu sync.Mutex
	queueCh chan struct{} // Signals new job in queue

	// Per-tenant job counts
	tenantJobs   map[string]int32
	tenantJobsMu sync.Mutex

	// Stats
	activeJobs   int64
	completedOK  int64
	completedErr int64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewScheduler creates a new job scheduler.
func NewScheduler(config SchedulerConfig, logger *slog.Logger) *Scheduler {
	if config.MaxParallelJobs <= 0 {
		config.MaxParallelJobs = 4
	}
	if config.MaxQueueSize <= 0 {
		config.MaxQueueSize = 100
	}
	if config.JobTimeout <= 0 {
		config.JobTimeout = 5 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Scheduler{
		config:     config,
		logger:     logger,
		sem:        make(chan struct{}, config.MaxParallelJobs),
		jobs:       make(map[string]*Job),
		queue:      make([]*Job, 0),
		queueCh:    make(chan struct{}, 1),
		tenantJobs: make(map[string]int32),
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start the dispatcher
	s.wg.Add(1)
	go s.dispatcher()

	return s
}

// Submit submits a job for execution.
// Returns error if queue is full.
func (s *Scheduler) Submit(job *Job) error {
	s.queueMu.Lock()

	// Check queue capacity
	if len(s.queue) >= s.config.MaxQueueSize {
		s.queueMu.Unlock()
		return errors.New("job queue is full")
	}

	// Add to queue (priority insertion)
	s.insertByPriority(job)

	s.queueMu.Unlock()

	// Register job
	s.jobsMu.Lock()
	s.jobs[job.ID] = job
	s.jobsMu.Unlock()

	// Signal dispatcher
	select {
	case s.queueCh <- struct{}{}:
	default:
	}

	s.logger.Debug("job submitted",
		"job_id", job.ID,
		"tenant_id", job.TenantID,
		"algo_kind", job.AlgoKind,
		"priority", job.Priority,
	)

	return nil
}

// insertByPriority inserts a job into the queue sorted by priority (higher first).
// Must be called with queueMu held.
func (s *Scheduler) insertByPriority(job *Job) {
	// Find insertion point (higher priority first)
	idx := len(s.queue)
	for i, j := range s.queue {
		if job.Priority > j.Priority {
			idx = i
			break
		}
	}

	// Insert at position
	s.queue = append(s.queue, nil)
	copy(s.queue[idx+1:], s.queue[idx:])
	s.queue[idx] = job
}

// dispatcher runs in a goroutine and dispatches jobs from the queue.
func (s *Scheduler) dispatcher() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.queueCh:
			s.dispatchPending()
		}
	}
}

// dispatchPending attempts to dispatch pending jobs.
func (s *Scheduler) dispatchPending() {
	for {
		// Try to acquire semaphore first (non-blocking)
		// This prevents a race where we pop from queue before knowing if we can run
		select {
		case s.sem <- struct{}{}:
			// Got a slot, now get a job from the queue
		default:
			// No slot available, return
			return
		}

		// Get next job from queue
		s.queueMu.Lock()
		if len(s.queue) == 0 {
			s.queueMu.Unlock()
			// Release the semaphore we just acquired
			<-s.sem
			return
		}
		job := s.queue[0]
		s.queue = s.queue[1:]
		s.queueMu.Unlock()

		// Check if job was canceled
		if job.State == JobStateCanceled {
			// Release semaphore and try next
			<-s.sem
			continue
		}

		// Execute the job
		s.wg.Add(1)
		go s.executeJob(job)
	}
}

// executeJob runs a job.
func (s *Scheduler) executeJob(job *Job) {
	defer s.wg.Done()
	defer func() { <-s.sem }() // Release semaphore

	// Update stats
	atomic.AddInt64(&s.activeJobs, 1)
	metrics.IncActiveJobs()
	defer func() {
		atomic.AddInt64(&s.activeJobs, -1)
		metrics.DecActiveJobs()
	}()

	// Track tenant jobs
	s.tenantJobsMu.Lock()
	s.tenantJobs[job.TenantID]++
	s.tenantJobsMu.Unlock()
	defer func() {
		s.tenantJobsMu.Lock()
		s.tenantJobs[job.TenantID]--
		if s.tenantJobs[job.TenantID] <= 0 {
			delete(s.tenantJobs, job.TenantID)
		}
		s.tenantJobsMu.Unlock()
	}()

	// Set up job context with timeout
	job.mu.Lock()
	job.ctx, job.cancel = context.WithTimeout(s.ctx, s.config.JobTimeout)
	job.State = JobStateRunning
	job.StartedAt = time.Now()
	job.mu.Unlock()

	defer job.cancel()

	s.logger.Debug("job started",
		"job_id", job.ID,
		"algo_kind", job.AlgoKind,
	)

	// Execute the job
	result, err := job.execFn(job.ctx)

	// Update job state
	job.mu.Lock()
	job.EndedAt = time.Now()
	if err != nil {
		job.State = JobStateFailed
		job.Error = err
		atomic.AddInt64(&s.completedErr, 1)
		s.logger.Warn("job failed",
			"job_id", job.ID,
			"error", err,
			"duration", job.EndedAt.Sub(job.StartedAt),
		)
	} else {
		job.State = JobStateSucceeded
		if result != nil {
			job.ResultID = result.ID
		}
		atomic.AddInt64(&s.completedOK, 1)
		s.logger.Debug("job succeeded",
			"job_id", job.ID,
			"result_id", job.ResultID,
			"duration", job.EndedAt.Sub(job.StartedAt),
		)
	}
	job.mu.Unlock()

	// Signal completion (safe close - may already be closed by Cancel)
	select {
	case <-job.done:
		// Already closed
	default:
		close(job.done)
	}

	// Try to dispatch more jobs
	select {
	case s.queueCh <- struct{}{}:
	default:
	}
}

// GetJob returns a job by ID.
func (s *Scheduler) GetJob(jobID string) (*Job, bool) {
	s.jobsMu.RLock()
	defer s.jobsMu.RUnlock()
	job, ok := s.jobs[jobID]
	return job, ok
}

// CancelJob cancels a job by ID.
func (s *Scheduler) CancelJob(jobID string) bool {
	s.jobsMu.RLock()
	job, ok := s.jobs[jobID]
	s.jobsMu.RUnlock()

	if !ok {
		return false
	}

	job.Cancel()
	return true
}

// Stats returns scheduler statistics.
func (s *Scheduler) Stats() SchedulerStats {
	s.queueMu.Lock()
	queueLen := len(s.queue)
	s.queueMu.Unlock()

	s.jobsMu.RLock()
	totalJobs := len(s.jobs)
	s.jobsMu.RUnlock()

	return SchedulerStats{
		ActiveJobs:      atomic.LoadInt64(&s.activeJobs),
		QueuedJobs:      int64(queueLen),
		TotalJobs:       int64(totalJobs),
		CompletedOK:     atomic.LoadInt64(&s.completedOK),
		CompletedErr:    atomic.LoadInt64(&s.completedErr),
		MaxParallelJobs: s.config.MaxParallelJobs,
		MaxQueueSize:    s.config.MaxQueueSize,
	}
}

// SchedulerStats holds scheduler statistics.
type SchedulerStats struct {
	ActiveJobs      int64
	QueuedJobs      int64
	TotalJobs       int64
	CompletedOK     int64
	CompletedErr    int64
	MaxParallelJobs int
	MaxQueueSize    int
}

// TenantJobCount returns the number of active jobs for a tenant.
func (s *Scheduler) TenantJobCount(tenantID string) int32 {
	s.tenantJobsMu.Lock()
	defer s.tenantJobsMu.Unlock()
	return s.tenantJobs[tenantID]
}

// Shutdown gracefully shuts down the scheduler.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	s.logger.Info("scheduler shutting down")

	// Signal shutdown
	s.cancel()

	// Wait for all jobs with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("scheduler shutdown complete")
		return nil
	case <-ctx.Done():
		s.logger.Warn("scheduler shutdown timed out")
		return ctx.Err()
	}
}

// RunSync executes a job synchronously (bypasses queue).
// Useful for high-priority or cache-hit scenarios.
func (s *Scheduler) RunSync(ctx context.Context, execFn func(ctx context.Context) (*AlgoResult, error)) (*AlgoResult, error) {
	// Acquire semaphore with context
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Update stats
	atomic.AddInt64(&s.activeJobs, 1)
	metrics.IncActiveJobs()
	defer func() {
		atomic.AddInt64(&s.activeJobs, -1)
		metrics.DecActiveJobs()
	}()

	// Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, s.config.JobTimeout)
	defer cancel()

	return execFn(execCtx)
}

// TryAcquire attempts to acquire a slot without blocking.
// Returns a release function if successful, nil otherwise.
func (s *Scheduler) TryAcquire() func() {
	select {
	case s.sem <- struct{}{}:
		atomic.AddInt64(&s.activeJobs, 1)
		metrics.IncActiveJobs()
		return func() {
			atomic.AddInt64(&s.activeJobs, -1)
			metrics.DecActiveJobs()
			<-s.sem
		}
	default:
		return nil
	}
}

// Acquire acquires a slot, blocking until available or context canceled.
// Returns a release function if successful.
func (s *Scheduler) Acquire(ctx context.Context) (func(), error) {
	select {
	case s.sem <- struct{}{}:
		atomic.AddInt64(&s.activeJobs, 1)
		metrics.IncActiveJobs()
		return func() {
			atomic.AddInt64(&s.activeJobs, -1)
			metrics.DecActiveJobs()
			<-s.sem
		}, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("failed to acquire scheduler slot: %w", ctx.Err())
	}
}

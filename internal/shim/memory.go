package shim

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	// ErrMemoryLimitExceeded is returned when an allocation would exceed the memory limit.
	ErrMemoryLimitExceeded = errors.New("memory limit exceeded")
	// ErrAllocationNotFound is returned when trying to deallocate an unknown allocation.
	ErrAllocationNotFound = errors.New("allocation not found")
	// ErrDoubleFree is returned when trying to deallocate the same allocation twice.
	ErrDoubleFree = errors.New("double free detected")
)

// -----------------------------------------------------------------------------
// Allocation tracking
// -----------------------------------------------------------------------------

// AllocationInfo stores metadata about a tracked allocation.
type AllocationInfo struct {
	ID        uint64    // Unique allocation ID
	Size      uint64    // Size in bytes
	Type      string    // Type of allocation (e.g., "graph", "view", "result")
	Label     string    // Optional label for debugging
	CreatedAt time.Time // When the allocation was made
}

// MemoryTracker tracks C-allocated memory for accounting and leak detection.
// It is safe for concurrent use.
type MemoryTracker struct {
	// Current memory usage (atomic for fast reads)
	currentBytes atomic.Uint64

	// Peak memory usage
	peakBytes atomic.Uint64

	// Memory limit (0 = unlimited)
	limitBytes uint64

	// Allocation tracking
	mu          sync.RWMutex
	allocations map[uint64]*AllocationInfo
	nextID      uint64

	// Counters for statistics
	totalAllocations   atomic.Uint64
	totalDeallocations atomic.Uint64
	totalBytesAlloc    atomic.Uint64
	totalBytesDealloc  atomic.Uint64

	// Callback for memory events (optional)
	onAlloc   func(info *AllocationInfo)
	onDealloc func(info *AllocationInfo)
}

// NewMemoryTracker creates a new memory tracker.
// limitBytes of 0 means unlimited.
func NewMemoryTracker(limitBytes uint64) *MemoryTracker {
	return &MemoryTracker{
		limitBytes:  limitBytes,
		allocations: make(map[uint64]*AllocationInfo),
		nextID:      1,
	}
}

// SetLimit updates the memory limit.
// Returns error if current usage already exceeds the new limit.
func (t *MemoryTracker) SetLimit(limitBytes uint64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limitBytes > 0 && t.currentBytes.Load() > limitBytes {
		return fmt.Errorf("%w: current usage %d exceeds new limit %d",
			ErrMemoryLimitExceeded, t.currentBytes.Load(), limitBytes)
	}

	t.limitBytes = limitBytes
	return nil
}

// Limit returns the current memory limit (0 = unlimited).
func (t *MemoryTracker) Limit() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.limitBytes
}

// Allocate records a new allocation.
// Returns an allocation ID and error if the limit would be exceeded.
func (t *MemoryTracker) Allocate(size uint64, allocType, label string) (uint64, error) {
	// Fast path: check limit without lock
	if t.limitBytes > 0 {
		current := t.currentBytes.Load()
		if current+size > t.limitBytes {
			return 0, fmt.Errorf("%w: current %d + requested %d > limit %d",
				ErrMemoryLimitExceeded, current, size, t.limitBytes)
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Re-check under lock
	current := t.currentBytes.Load()
	if t.limitBytes > 0 && current+size > t.limitBytes {
		return 0, fmt.Errorf("%w: current %d + requested %d > limit %d",
			ErrMemoryLimitExceeded, current, size, t.limitBytes)
	}

	// Assign ID and create allocation info
	id := t.nextID
	t.nextID++

	info := &AllocationInfo{
		ID:        id,
		Size:      size,
		Type:      allocType,
		Label:     label,
		CreatedAt: time.Now(),
	}

	t.allocations[id] = info

	// Update counters
	newCurrent := t.currentBytes.Add(size)
	t.totalAllocations.Add(1)
	t.totalBytesAlloc.Add(size)

	// Update peak
	for {
		peak := t.peakBytes.Load()
		if newCurrent <= peak {
			break
		}
		if t.peakBytes.CompareAndSwap(peak, newCurrent) {
			break
		}
	}

	// Callback
	if t.onAlloc != nil {
		t.onAlloc(info)
	}

	return id, nil
}

// Deallocate removes a tracked allocation.
// Returns error if the allocation is not found.
func (t *MemoryTracker) Deallocate(id uint64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	info, ok := t.allocations[id]
	if !ok {
		return fmt.Errorf("%w: id %d", ErrAllocationNotFound, id)
	}

	delete(t.allocations, id)

	// Update counters
	t.currentBytes.Add(^(info.Size - 1)) // Subtract size
	t.totalDeallocations.Add(1)
	t.totalBytesDealloc.Add(info.Size)

	// Callback
	if t.onDealloc != nil {
		t.onDealloc(info)
	}

	return nil
}

// DeallocateBySize removes an allocation by type and size (for anonymous allocations).
// This is useful when the allocation ID is not available.
// Returns error if no matching allocation is found.
func (t *MemoryTracker) DeallocateBySize(size uint64, allocType string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Find the first matching allocation
	var foundID uint64
	var foundInfo *AllocationInfo

	for id, info := range t.allocations {
		if info.Size == size && info.Type == allocType {
			foundID = id
			foundInfo = info
			break
		}
	}

	if foundInfo == nil {
		return fmt.Errorf("%w: size %d type %s", ErrAllocationNotFound, size, allocType)
	}

	delete(t.allocations, foundID)

	// Update counters
	t.currentBytes.Add(^(foundInfo.Size - 1)) // Subtract size
	t.totalDeallocations.Add(1)
	t.totalBytesDealloc.Add(foundInfo.Size)

	// Callback
	if t.onDealloc != nil {
		t.onDealloc(foundInfo)
	}

	return nil
}

// Current returns the current memory usage in bytes.
func (t *MemoryTracker) Current() uint64 {
	return t.currentBytes.Load()
}

// Peak returns the peak memory usage in bytes.
func (t *MemoryTracker) Peak() uint64 {
	return t.peakBytes.Load()
}

// Available returns the available memory (limit - current).
// Returns MaxUint64 if unlimited.
func (t *MemoryTracker) Available() uint64 {
	t.mu.RLock()
	limit := t.limitBytes
	t.mu.RUnlock()

	if limit == 0 {
		return ^uint64(0) // MaxUint64
	}

	current := t.currentBytes.Load()
	if current >= limit {
		return 0
	}
	return limit - current
}

// CanAllocate checks if an allocation of the given size would succeed.
func (t *MemoryTracker) CanAllocate(size uint64) bool {
	t.mu.RLock()
	limit := t.limitBytes
	t.mu.RUnlock()

	if limit == 0 {
		return true
	}

	return t.currentBytes.Load()+size <= limit
}

// -----------------------------------------------------------------------------
// Statistics
// -----------------------------------------------------------------------------

// MemoryStats contains memory tracker statistics.
type MemoryStats struct {
	CurrentBytes       uint64
	PeakBytes          uint64
	LimitBytes         uint64
	TotalAllocations   uint64
	TotalDeallocations uint64
	TotalBytesAlloc    uint64
	TotalBytesDealloc  uint64
	ActiveAllocations  int
}

// Stats returns current memory statistics.
func (t *MemoryTracker) Stats() MemoryStats {
	t.mu.RLock()
	activeCount := len(t.allocations)
	limit := t.limitBytes
	t.mu.RUnlock()

	return MemoryStats{
		CurrentBytes:       t.currentBytes.Load(),
		PeakBytes:          t.peakBytes.Load(),
		LimitBytes:         limit,
		TotalAllocations:   t.totalAllocations.Load(),
		TotalDeallocations: t.totalDeallocations.Load(),
		TotalBytesAlloc:    t.totalBytesAlloc.Load(),
		TotalBytesDealloc:  t.totalBytesDealloc.Load(),
		ActiveAllocations:  activeCount,
	}
}

// ActiveAllocations returns a copy of all active allocations.
func (t *MemoryTracker) ActiveAllocations() []*AllocationInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*AllocationInfo, 0, len(t.allocations))
	for _, info := range t.allocations {
		// Copy to avoid races
		infoCopy := *info
		result = append(result, &infoCopy)
	}
	return result
}

// ActiveAllocationsByType returns active allocations filtered by type.
func (t *MemoryTracker) ActiveAllocationsByType(allocType string) []*AllocationInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]*AllocationInfo, 0)
	for _, info := range t.allocations {
		if info.Type == allocType {
			infoCopy := *info
			result = append(result, &infoCopy)
		}
	}
	return result
}

// CountByType returns the count and total bytes for a specific allocation type.
func (t *MemoryTracker) CountByType(allocType string) (count int, totalBytes uint64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, info := range t.allocations {
		if info.Type == allocType {
			count++
			totalBytes += info.Size
		}
	}
	return
}

// -----------------------------------------------------------------------------
// Leak detection
// -----------------------------------------------------------------------------

// LeakReport contains information about potential memory leaks.
type LeakReport struct {
	HasLeaks       bool
	LeakedBytes    uint64
	LeakedCount    int
	OldAllocations []*AllocationInfo // Allocations older than threshold
}

// CheckLeaks returns a report of potential memory leaks.
// ageThreshold specifies the minimum age for an allocation to be considered a potential leak.
func (t *MemoryTracker) CheckLeaks(ageThreshold time.Duration) *LeakReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	report := &LeakReport{
		OldAllocations: make([]*AllocationInfo, 0),
	}

	for _, info := range t.allocations {
		age := now.Sub(info.CreatedAt)
		if age >= ageThreshold {
			infoCopy := *info
			report.OldAllocations = append(report.OldAllocations, &infoCopy)
			report.LeakedBytes += info.Size
			report.LeakedCount++
		}
	}

	report.HasLeaks = report.LeakedCount > 0
	return report
}

// Reset clears all tracking state. Use with caution.
// This does NOT free the underlying C memory - it only resets the tracker.
func (t *MemoryTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.allocations = make(map[uint64]*AllocationInfo)
	t.currentBytes.Store(0)
	t.peakBytes.Store(0)
	t.totalAllocations.Store(0)
	t.totalDeallocations.Store(0)
	t.totalBytesAlloc.Store(0)
	t.totalBytesDealloc.Store(0)
	t.nextID = 1
}

// -----------------------------------------------------------------------------
// Callbacks
// -----------------------------------------------------------------------------

// SetCallbacks sets optional callbacks for allocation events.
// Pass nil to disable a callback.
func (t *MemoryTracker) SetCallbacks(onAlloc, onDealloc func(*AllocationInfo)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onAlloc = onAlloc
	t.onDealloc = onDealloc
}

// -----------------------------------------------------------------------------
// TrackedGraph - Graph with memory tracking
// -----------------------------------------------------------------------------

// TrackedGraph wraps a Graph with automatic memory tracking.
type TrackedGraph struct {
	*Graph
	tracker *MemoryTracker
	allocID uint64
}

// NewTrackedGraph creates a new graph with memory tracking.
func NewTrackedGraph(tracker *MemoryTracker, n uint32, src, dst []uint32, directed bool) (*TrackedGraph, error) {
	// Estimate memory before creating
	estimatedSize := estimateGraphMemory(n, uint64(len(src)))

	// Check if we can allocate
	allocID, err := tracker.Allocate(estimatedSize, "graph", "")
	if err != nil {
		return nil, err
	}

	// Create the graph
	g, err := NewGraph(n, src, dst, directed)
	if err != nil {
		// Release the allocation on failure
		if deallocErr := tracker.Deallocate(allocID); deallocErr != nil {
			log.Printf("Deallocate failed: %v", deallocErr)
		}
		return nil, err
	}

	// Update with actual memory size
	actualSize := g.MemoryBytes()
	if actualSize != estimatedSize {
		if deallocErr := tracker.Deallocate(allocID); deallocErr != nil {
			log.Printf("Deallocate failed: %v", deallocErr)
		}
		allocID, err = tracker.Allocate(actualSize, "graph", "")
		if err != nil {
			g.Close()
			return nil, err
		}
	}

	return &TrackedGraph{
		Graph:   g,
		tracker: tracker,
		allocID: allocID,
	}, nil
}

// Close releases the graph and removes it from memory tracking.
func (tg *TrackedGraph) Close() {
	if tg.Graph != nil {
		tg.Graph.Close()
		if err := tg.tracker.Deallocate(tg.allocID); err != nil {
			log.Printf("Deallocate failed: %v", err)
		}
		tg.Graph = nil
	}
}

// Tracker returns the memory tracker.
func (tg *TrackedGraph) Tracker() *MemoryTracker {
	return tg.tracker
}

// -----------------------------------------------------------------------------
// TrackedView - View with memory tracking
// -----------------------------------------------------------------------------

// TrackedView wraps a View with automatic memory tracking.
type TrackedView struct {
	*View
	tracker *MemoryTracker
	allocID uint64
}

// NewTrackedViewFromEdgeMask creates a view with memory tracking.
func (tg *TrackedGraph) NewTrackedViewFromEdgeMask(edgeMask []byte) (*TrackedView, error) {
	// Estimate memory
	estimatedSize := uint64(len(edgeMask) * 32) // Rough estimate

	allocID, err := tg.tracker.Allocate(estimatedSize, "view", "")
	if err != nil {
		return nil, err
	}

	v, err := tg.Graph.NewViewFromEdgeMask(edgeMask) //nolint:staticcheck // keep explicit Graph selector
	if err != nil {
		if deallocErr := tg.tracker.Deallocate(allocID); deallocErr != nil {
			log.Printf("Deallocate failed: %v", deallocErr)
		}
		return nil, err
	}

	// Update with actual size
	actualSize := v.MemoryBytes()
	if actualSize != estimatedSize {
		if deallocErr := tg.tracker.Deallocate(allocID); deallocErr != nil {
			log.Printf("Deallocate failed: %v", deallocErr)
		}
		allocID, err = tg.tracker.Allocate(actualSize, "view", "")
		if err != nil {
			v.Close()
			return nil, err
		}
	}

	return &TrackedView{
		View:    v,
		tracker: tg.tracker,
		allocID: allocID,
	}, nil
}

// Close releases the view and removes it from memory tracking.
func (tv *TrackedView) Close() {
	if tv.View != nil {
		tv.View.Close()
		if err := tv.tracker.Deallocate(tv.allocID); err != nil {
			log.Printf("Deallocate failed: %v", err)
		}
		tv.View = nil
	}
}

// -----------------------------------------------------------------------------
// TrackedResult - Result with memory tracking
// -----------------------------------------------------------------------------

// TrackedResult wraps a Result with automatic memory tracking.
type TrackedResult struct {
	*Result
	tracker *MemoryTracker
	allocID uint64
}

// trackResult wraps a result with memory tracking.
//
//nolint:unused // reserved for future use
func trackResult(tracker *MemoryTracker, r *Result) (*TrackedResult, error) {
	size := r.MemoryBytes()

	allocID, err := tracker.Allocate(size, "result", "")
	if err != nil {
		r.Close()
		return nil, err
	}

	return &TrackedResult{
		Result:  r,
		tracker: tracker,
		allocID: allocID,
	}, nil
}

// Close releases the result and removes it from memory tracking.
func (tr *TrackedResult) Close() {
	if tr.Result != nil {
		tr.Result.Close()
		if err := tr.tracker.Deallocate(tr.allocID); err != nil {
			log.Printf("Deallocate failed: %v", err)
		}
		tr.Result = nil
	}
}

// -----------------------------------------------------------------------------
// Memory estimation helpers
// -----------------------------------------------------------------------------

// estimateGraphMemory estimates the memory footprint of a graph.
// Based on igraph internals: ~24 bytes per vertex + ~8 bytes per edge.
func estimateGraphMemory(vertices uint32, edges uint64) uint64 {
	const (
		bytesPerVertex = 24
		bytesPerEdge   = 8
		baseOverhead   = 256 // Base structure overhead
	)
	return uint64(vertices)*bytesPerVertex + edges*bytesPerEdge + baseOverhead
}

// estimateResultMemory estimates the memory footprint of a result.
//
//nolint:unused // reserved for future use
func estimateResultMemory(numBuffers int, totalElements uint64, avgElemSize uint64) uint64 {
	const bufferOverhead = 64 // Per-buffer overhead
	return uint64(numBuffers)*bufferOverhead + totalElements*avgElemSize
}

// -----------------------------------------------------------------------------
// Global default tracker
// -----------------------------------------------------------------------------

var (
	defaultTracker     *MemoryTracker
	defaultTrackerOnce sync.Once
)

// DefaultTracker returns the global default memory tracker.
// It is created with no limit on first access.
func DefaultTracker() *MemoryTracker {
	defaultTrackerOnce.Do(func() {
		defaultTracker = NewMemoryTracker(0)
	})
	return defaultTracker
}

// SetDefaultTracker sets the global default memory tracker.
// Must be called before any use of DefaultTracker().
func SetDefaultTracker(t *MemoryTracker) {
	defaultTracker = t
}

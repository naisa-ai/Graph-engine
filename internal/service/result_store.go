package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/google/uuid"
)

// TraceSpan represents a timing span during algorithm execution.
type TraceSpan struct {
	Name      string
	StartTime time.Time
	Duration  time.Duration
	Tags      map[string]string
}

// NewTraceSpan creates a new trace span starting now.
func NewTraceSpan(name string) *TraceSpan {
	return &TraceSpan{
		Name:      name,
		StartTime: time.Now(),
		Tags:      make(map[string]string),
	}
}

// End marks the span as complete.
func (s *TraceSpan) End() {
	s.Duration = time.Since(s.StartTime)
}

// AddTag adds a tag to the span.
func (s *TraceSpan) AddTag(key, value string) *TraceSpan {
	s.Tags[key] = value
	return s
}

// AlgoKind represents the type of algorithm.
type AlgoKind string

// KSPPath represents a single path in K-shortest paths result.
type KSPPath struct {
	Vertices []uint64
	Edges    []uint64
	Cost     float64
}

const (
	AlgoKindComponents   AlgoKind = "components"
	AlgoKindCommunities  AlgoKind = "communities"
	AlgoKindShortestPath AlgoKind = "shortest_path"
	AlgoKindKSP          AlgoKind = "k_shortest_paths"
	AlgoKindDistances    AlgoKind = "distances"
	AlgoKindBFS          AlgoKind = "bfs"
	AlgoKindNeighborhood AlgoKind = "neighborhood"
	AlgoKindSTMinCut     AlgoKind = "st_mincut"
	AlgoKindCorridor     AlgoKind = "corridor"
	AlgoKindKCore        AlgoKind = "kcore"
	AlgoKindBetweenness  AlgoKind = "betweenness"
	AlgoKindCloseness    AlgoKind = "closeness"
	AlgoKindPageRank     AlgoKind = "pagerank"
)

// AlgoResult represents the result of an algorithm execution.
type AlgoResult struct {
	// Identity
	ID        string
	VersionID string
	AlgoKind  AlgoKind
	CreatedAt time.Time

	// Cache key components
	ParamsHash string

	// Result data (depending on algorithm)
	MembershipU32 []uint32  // For components/communities
	PathVertices  []uint64  // For shortest path
	PathEdges     []uint64  // For shortest path
	PathCost      float64   // For shortest path
	DistancesF64  []float64 // For distances
	VerticesU64   []uint64  // For BFS/Neighborhood result vertices
	CutValue      float64   // For st_mincut
	CutEdges      []uint64  // For st_mincut

	// K-shortest paths results
	KSPPaths []*KSPPath // For k_shortest_paths

	// K-Core results
	NodeIDsU64  []uint64 // External node IDs (parallel with CorenessU32)
	CorenessU32 []uint32 // Coreness value per vertex
	MaxCore     uint32   // Maximum k found

	// Betweenness centrality results
	BetweennessF64 []float64 // Betweenness score per vertex

	// Closeness centrality results
	ClosenessF64 []float64 // Closeness score per vertex

	// PageRank results
	PageRankF64       []float64 // PageRank score per vertex
	PageRankIterations uint32   // Number of iterations performed
	PageRankConverged  bool     // Whether the algorithm converged

	// Trace spans for observability
	Spans []*TraceSpan

	// Metadata
	Meta map[string]string

	// Reference counting
	refCount int
	refMu    sync.Mutex

	// Memory tracking
	memoryBytes uint64

	// LRU tracking
	lastAccess time.Time
}

// NewAlgoResult creates a new AlgoResult.
func NewAlgoResult(versionID string, algoKind AlgoKind, paramsHash string) *AlgoResult {
	return &AlgoResult{
		ID:         uuid.New().String(),
		VersionID:  versionID,
		AlgoKind:   algoKind,
		ParamsHash: paramsHash,
		CreatedAt:  time.Now(),
		lastAccess: time.Now(),
		Meta:       make(map[string]string),
		Spans:      make([]*TraceSpan, 0),
	}
}

// AddSpan adds a completed trace span to the result.
func (r *AlgoResult) AddSpan(span *TraceSpan) {
	r.Spans = append(r.Spans, span)
}

// StartSpan creates and starts a new trace span, returning it for deferred End() call.
func (r *AlgoResult) StartSpan(name string) *TraceSpan {
	span := NewTraceSpan(name)
	return span
}

// RecordSpan records a span with a given duration (for already-timed operations).
func (r *AlgoResult) RecordSpan(name string, duration time.Duration, tags map[string]string) {
	span := &TraceSpan{
		Name:      name,
		StartTime: time.Now().Add(-duration),
		Duration:  duration,
		Tags:      tags,
	}
	if span.Tags == nil {
		span.Tags = make(map[string]string)
	}
	r.Spans = append(r.Spans, span)
}

// Pin increments the reference count.
func (r *AlgoResult) Pin() {
	r.refMu.Lock()
	r.refCount++
	r.lastAccess = time.Now()
	r.refMu.Unlock()
}

// Unpin decrements the reference count.
func (r *AlgoResult) Unpin() bool {
	r.refMu.Lock()
	defer r.refMu.Unlock()
	r.refCount--
	return r.refCount <= 0
}

// RefCount returns the current reference count.
func (r *AlgoResult) RefCount() int {
	r.refMu.Lock()
	defer r.refMu.Unlock()
	return r.refCount
}

// EstimateMemory calculates memory usage in bytes.
func (r *AlgoResult) EstimateMemory() uint64 {
	if r.memoryBytes > 0 {
		return r.memoryBytes
	}

	var total uint64

	// Array data
	total += uint64(len(r.MembershipU32)) * 4
	total += uint64(len(r.PathVertices)) * 8
	total += uint64(len(r.PathEdges)) * 8
	total += uint64(len(r.DistancesF64)) * 8
	total += uint64(len(r.VerticesU64)) * 8
	total += uint64(len(r.CutEdges)) * 8

	// KSP paths
	for _, p := range r.KSPPaths {
		total += uint64(len(p.Vertices)) * 8
		total += uint64(len(p.Edges)) * 8
		total += 8 // Cost float64
		total += uint64(unsafe.Sizeof(*p))
	}

	// K-Core data
	total += uint64(len(r.NodeIDsU64)) * 8
	total += uint64(len(r.CorenessU32)) * 4

	// Betweenness data
	total += uint64(len(r.BetweennessF64)) * 8

	// Closeness data
	total += uint64(len(r.ClosenessF64)) * 8

	// PageRank data
	total += uint64(len(r.PageRankF64)) * 8

	// Struct overhead
	total += uint64(unsafe.Sizeof(*r))

	r.memoryBytes = total
	return total
}

// CacheKey returns the cache key for this result.
func (r *AlgoResult) CacheKey() string {
	return fmt.Sprintf("%s:%s:%s", r.VersionID, r.AlgoKind, r.ParamsHash)
}

// resultEntry wraps an AlgoResult with LRU metadata.
type resultEntry struct {
	result     *AlgoResult
	lastAccess time.Time
}

// ResultStore stores algorithm results with caching and LRU eviction.
type ResultStore struct {
	// Results by ID
	results map[string]*resultEntry
	// Cache index: cacheKey -> resultID
	cacheIndex map[string]string
	mu         sync.RWMutex

	// Limits
	maxItems    int
	maxMemory   uint64
	ttl         time.Duration
	totalMemory uint64

	// Shutdown
	stopCh chan struct{}
}

// NewResultStore creates a new ResultStore.
func NewResultStore(maxItems int, maxMemory uint64, ttl time.Duration) *ResultStore {
	rs := &ResultStore{
		results:    make(map[string]*resultEntry),
		cacheIndex: make(map[string]string),
		maxItems:   maxItems,
		maxMemory:  maxMemory,
		ttl:        ttl,
		stopCh:     make(chan struct{}),
	}

	// Start background cleanup
	go rs.cleanupLoop()

	return rs
}

// Close stops the background cleanup goroutine.
func (rs *ResultStore) Close() {
	close(rs.stopCh)
}

// Store adds a result to the store.
func (rs *ResultStore) Store(result *AlgoResult) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Check if already exists
	if _, exists := rs.results[result.ID]; exists {
		return fmt.Errorf("result already exists: %s", result.ID)
	}

	// Evict if needed
	rs.evictIfNeeded(result.EstimateMemory())

	// Store result
	entry := &resultEntry{
		result:     result,
		lastAccess: time.Now(),
	}
	rs.results[result.ID] = entry
	rs.totalMemory += result.EstimateMemory()

	// Add to cache index
	cacheKey := result.CacheKey()
	rs.cacheIndex[cacheKey] = result.ID

	return nil
}

// Get retrieves and pins a result by ID.
func (rs *ResultStore) Get(resultID string) (*AlgoResult, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	entry, exists := rs.results[resultID]
	if !exists {
		return nil, fmt.Errorf("result not found: %s", resultID)
	}

	entry.lastAccess = time.Now()
	entry.result.Pin()

	return entry.result, nil
}

// GetByKey retrieves a cached result by cache key.
func (rs *ResultStore) GetByKey(versionID string, algoKind AlgoKind, paramsHash string) (*AlgoResult, bool) {
	cacheKey := fmt.Sprintf("%s:%s:%s", versionID, algoKind, paramsHash)

	rs.mu.Lock()
	defer rs.mu.Unlock()

	resultID, exists := rs.cacheIndex[cacheKey]
	if !exists {
		return nil, false
	}

	entry, exists := rs.results[resultID]
	if !exists {
		// Stale cache entry
		delete(rs.cacheIndex, cacheKey)
		return nil, false
	}

	entry.lastAccess = time.Now()
	entry.result.Pin()

	return entry.result, true
}

// HashParams creates a hash from algorithm parameters for cache key.
func HashParams(params ...interface{}) string {
	h := sha256.New()
	for _, p := range params {
		h.Write([]byte(fmt.Sprintf("%v", p)))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// evictIfNeeded removes old entries to make room for new ones.
// Must be called with lock held.
func (rs *ResultStore) evictIfNeeded(newBytes uint64) {
	// Check item limit
	for len(rs.results) >= rs.maxItems {
		if !rs.evictLRU() {
			break // No more unpinned items to evict
		}
	}

	// Check memory limit
	for rs.maxMemory > 0 && rs.totalMemory+newBytes > rs.maxMemory {
		if !rs.evictLRU() {
			break // No more to evict
		}
	}
}

// evictLRU removes the least recently used unpinned entry.
// Must be called with lock held. Returns false if nothing to evict.
func (rs *ResultStore) evictLRU() bool {
	var oldestID string
	var oldestTime time.Time

	for id, entry := range rs.results {
		if entry.result.RefCount() > 0 {
			continue // Skip pinned
		}
		if oldestID == "" || entry.lastAccess.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.lastAccess
		}
	}

	if oldestID == "" {
		return false
	}

	entry := rs.results[oldestID]
	rs.totalMemory -= entry.result.EstimateMemory()
	delete(rs.cacheIndex, entry.result.CacheKey())
	delete(rs.results, oldestID)

	return true
}

// cleanupLoop periodically removes expired results.
func (rs *ResultStore) cleanupLoop() {
	// Use a minimum interval of 1 second to avoid tight loops in tests
	interval := rs.ttl / 2
	if interval < time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-rs.stopCh:
			return
		case <-ticker.C:
			rs.cleanup()
		}
	}
}

// cleanup removes expired unpinned results.
func (rs *ResultStore) cleanup() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	now := time.Now()
	for id, entry := range rs.results {
		if entry.result.RefCount() > 0 {
			continue
		}
		if now.Sub(entry.lastAccess) > rs.ttl {
			rs.totalMemory -= entry.result.EstimateMemory()
			delete(rs.cacheIndex, entry.result.CacheKey())
			delete(rs.results, id)
		}
	}
}

// Count returns the number of results in the store.
func (rs *ResultStore) Count() int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return len(rs.results)
}

// TotalMemory returns total memory usage.
func (rs *ResultStore) TotalMemory() uint64 {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.totalMemory
}

// Stats returns store statistics.
func (rs *ResultStore) Stats() ResultStoreStats {
	rs.mu.RLock()
	defer rs.mu.RUnlock()

	return ResultStoreStats{
		TotalItems:  len(rs.results),
		TotalMemory: rs.totalMemory,
		MaxItems:    rs.maxItems,
		MaxMemory:   rs.maxMemory,
	}
}

// ResultStoreStats contains statistics about the result store.
type ResultStoreStats struct {
	TotalItems  int
	TotalMemory uint64
	MaxItems    int
	MaxMemory   uint64
}

// Delete removes a result by ID.
func (rs *ResultStore) Delete(resultID string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	entry, exists := rs.results[resultID]
	if !exists {
		return fmt.Errorf("result not found: %s", resultID)
	}

	if entry.result.RefCount() > 0 {
		return fmt.Errorf("result still pinned: %s", resultID)
	}

	rs.totalMemory -= entry.result.EstimateMemory()
	delete(rs.cacheIndex, entry.result.CacheKey())
	delete(rs.results, resultID)

	return nil
}

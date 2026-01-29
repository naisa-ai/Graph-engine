// Package metrics provides Prometheus metrics for the graph-engine service.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "graph_engine"
)

// Store labels for cache metrics
const (
	StoreResult  = "result"
	StoreView    = "view"
	StoreVersion = "version"
)

var (
	// RPCDuration tracks the duration of RPC calls in seconds.
	RPCDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "rpc_duration_seconds",
			Help:      "Duration of RPC calls in seconds",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "status"},
	)

	// CacheHits tracks the total number of cache hits.
	CacheHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_hits_total",
			Help:      "Total number of cache hits",
		},
		[]string{"store"},
	)

	// CacheMisses tracks the total number of cache misses.
	CacheMisses = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "cache_misses_total",
			Help:      "Total number of cache misses",
		},
		[]string{"store"},
	)

	// MemoryBytes tracks the memory usage in bytes by store type.
	MemoryBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "memory_bytes",
			Help:      "Memory usage in bytes by store type",
		},
		[]string{"store"},
	)

	// ActiveJobs tracks the number of currently active jobs.
	ActiveJobs = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_jobs",
			Help:      "Number of currently active jobs",
		},
	)

	// GraphVersions tracks the number of versions per graph.
	GraphVersions = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "graph_versions_total",
			Help:      "Number of versions per graph",
		},
		[]string{"graph_name"},
	)

	// CacheItems tracks the number of items in each cache.
	CacheItems = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cache_items_total",
			Help:      "Number of items in each cache",
		},
		[]string{"store"},
	)

	// BuildDuration tracks the duration of graph build operations.
	BuildDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "build_duration_seconds",
			Help:      "Duration of graph build operations in seconds",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
	)

	// AlgoDuration tracks the duration of algorithm executions by algorithm kind.
	AlgoDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "algo_duration_seconds",
			Help:      "Duration of algorithm executions in seconds",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		},
		[]string{"algo_kind"},
	)

	// RequestsTotal tracks total requests by method.
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_total",
			Help:      "Total number of requests by method",
		},
		[]string{"method"},
	)

	// ErrorsTotal tracks total errors by method and error code.
	ErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "errors_total",
			Help:      "Total number of errors by method and code",
		},
		[]string{"method", "code"},
	)
)

// RecordRPCDuration records the duration of an RPC call.
func RecordRPCDuration(method, status string, durationSeconds float64) {
	RPCDuration.WithLabelValues(method, status).Observe(durationSeconds)
}

// RecordCacheHit records a cache hit for the specified store.
func RecordCacheHit(store string) {
	CacheHits.WithLabelValues(store).Inc()
}

// RecordCacheMiss records a cache miss for the specified store.
func RecordCacheMiss(store string) {
	CacheMisses.WithLabelValues(store).Inc()
}

// IncCacheHits is an alias for RecordCacheHit with algorithm as the store name.
func IncCacheHits(algo string) {
	CacheHits.WithLabelValues(algo).Inc()
}

// IncCacheMisses is an alias for RecordCacheMiss with algorithm as the store name.
func IncCacheMisses(algo string) {
	CacheMisses.WithLabelValues(algo).Inc()
}

// SetMemoryBytes sets the memory usage for a store.
func SetMemoryBytes(store string, bytes uint64) {
	MemoryBytes.WithLabelValues(store).Set(float64(bytes))
}

// SetCacheItems sets the number of items in a cache.
func SetCacheItems(store string, count int) {
	CacheItems.WithLabelValues(store).Set(float64(count))
}

// IncActiveJobs increments the active jobs counter.
func IncActiveJobs() {
	ActiveJobs.Inc()
}

// DecActiveJobs decrements the active jobs counter.
func DecActiveJobs() {
	ActiveJobs.Dec()
}

// SetGraphVersions sets the number of versions for a graph.
func SetGraphVersions(graphName string, count int) {
	GraphVersions.WithLabelValues(graphName).Set(float64(count))
}

// RecordBuildDuration records the duration of a build operation.
func RecordBuildDuration(durationSeconds float64) {
	BuildDuration.Observe(durationSeconds)
}

// RecordAlgoDuration records the duration of an algorithm execution.
func RecordAlgoDuration(algoKind string, durationSeconds float64) {
	AlgoDuration.WithLabelValues(algoKind).Observe(durationSeconds)
}

// IncRequests increments the requests counter for a method.
func IncRequests(method string) {
	RequestsTotal.WithLabelValues(method).Inc()
}

// IncErrors increments the errors counter for a method and error code.
func IncErrors(method, code string) {
	ErrorsTotal.WithLabelValues(method, code).Inc()
}

// CacheStats holds cache statistics for reporting.
type CacheStats struct {
	Items  int
	Bytes  uint64
	Hits   uint64
	Misses uint64
}

// UpdateCacheStats updates all cache-related metrics for a store.
func UpdateCacheStats(store string, stats CacheStats) {
	CacheItems.WithLabelValues(store).Set(float64(stats.Items))
	MemoryBytes.WithLabelValues(store).Set(float64(stats.Bytes))
}

// Package config provides configuration loading for graph-engine.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration for the graph-engine service.
type Config struct {
	Server ServerConfig `yaml:"server"`
	Log    LogConfig    `yaml:"log"`
	TLS    TLSConfig    `yaml:"tls"`
	Auth   AuthConfig   `yaml:"auth"`
	Limits LimitsConfig `yaml:"limits"`
	Igraph IgraphConfig `yaml:"igraph"`
}

// ServerConfig holds server-related settings.
type ServerConfig struct {
	GRPCPort int `yaml:"grpc_port"`
	HTTPPort int `yaml:"http_port"`
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// TLSConfig holds TLS settings.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Enabled bool `yaml:"enabled"`
}

// LimitsConfig holds resource limit settings.
type LimitsConfig struct {
	MaxGraphs              int    `yaml:"max_graphs"`
	MaxBuildsPending       int    `yaml:"max_builds_pending"`
	MaxParallelJobs        int    `yaml:"max_parallel_jobs"`
	MaxConcurrentPerTenant int    `yaml:"max_concurrent_per_tenant"`
	MaxGlobalConcurrent    int    `yaml:"max_global_concurrent"`
	MaxExportEdges         int    `yaml:"max_export_edges"`
	MaxValidationVertices  uint64 `yaml:"max_validation_vertices"`
	MaxValidationEdges     uint64 `yaml:"max_validation_edges"`
	VersionStoreMemoryMB   int    `yaml:"version_store_memory_mb"`
	VersionCleanupTTLSec   int    `yaml:"version_cleanup_ttl_sec"`
	ViewStoreMemoryMB      int    `yaml:"view_store_memory_mb"`
	ResultStoreMemoryMB    int    `yaml:"result_store_memory_mb"`
}

// GetVersionStoreMemoryBytes returns the version store memory limit in bytes.
func (c *LimitsConfig) GetVersionStoreMemoryBytes() uint64 {
	if c.VersionStoreMemoryMB <= 0 {
		return 1 << 30 // 1 GB default
	}
	return uint64(c.VersionStoreMemoryMB) << 20
}

// GetVersionCleanupTTL returns the version cleanup TTL as a time.Duration.
func (c *LimitsConfig) GetVersionCleanupTTL() time.Duration {
	if c.VersionCleanupTTLSec <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.VersionCleanupTTLSec) * time.Second
}

// GetViewStoreMemoryBytes returns the view store memory limit in bytes.
func (c *LimitsConfig) GetViewStoreMemoryBytes() uint64 {
	if c.ViewStoreMemoryMB <= 0 {
		return 200 << 20 // 200 MB default
	}
	return uint64(c.ViewStoreMemoryMB) << 20
}

// GetResultStoreMemoryBytes returns the result store memory limit in bytes.
func (c *LimitsConfig) GetResultStoreMemoryBytes() uint64 {
	if c.ResultStoreMemoryMB <= 0 {
		return 500 << 20 // 500 MB default
	}
	return uint64(c.ResultStoreMemoryMB) << 20
}

// GetMaxValidationVertices returns the effective max validation vertices.
// Returns 1_000_000 if not configured or set to 0.
func (c *LimitsConfig) GetMaxValidationVertices() uint64 {
	if c.MaxValidationVertices == 0 {
		return 1_000_000
	}
	return c.MaxValidationVertices
}

// GetMaxValidationEdges returns the effective max validation edges.
// Returns 10_000_000 if not configured or set to 0.
func (c *LimitsConfig) GetMaxValidationEdges() uint64 {
	if c.MaxValidationEdges == 0 {
		return 10_000_000
	}
	return c.MaxValidationEdges
}

// IgraphConfig holds igraph shim layer settings.
type IgraphConfig struct {
	// MaxParallelCallsPerVersion limits concurrent igraph algorithm executions
	// per graph version. This is important because igraph thread-safety depends
	// on build configuration. Start with 1; increase after validation.
	// 0 means use default (1).
	MaxParallelCallsPerVersion int `yaml:"max_parallel_calls_per_version"`

	// MemoryLimitBytes is the maximum memory the igraph shim can allocate
	// across all graphs, views, and results. 0 means unlimited.
	MemoryLimitBytes uint64 `yaml:"memory_limit_bytes"`

	// EnableShim controls whether to use the igraph C shim for algorithms.
	// If false, falls back to pure Go implementations.
	EnableShim bool `yaml:"enable_shim"`

	// FallbackOnError controls whether to fall back to pure Go implementations
	// when the igraph shim encounters an error.
	FallbackOnError bool `yaml:"fallback_on_error"`
}

// DefaultConfig returns a Config with default values.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			GRPCPort: 50051,
			HTTPPort: 8080,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		TLS: TLSConfig{
			Enabled: false,
		},
		Auth: AuthConfig{
			Enabled: false,
		},
		Limits: LimitsConfig{
			MaxGraphs:              10,
			MaxBuildsPending:       5,
			MaxParallelJobs:        4,
			MaxConcurrentPerTenant: 10,
			MaxGlobalConcurrent:    100,
			MaxExportEdges:         10000,
			MaxValidationVertices:  1_000_000,
			MaxValidationEdges:     10_000_000,
			VersionStoreMemoryMB:   1024,
			VersionCleanupTTLSec:   300,
			ViewStoreMemoryMB:      200,
			ResultStoreMemoryMB:    500,
		},
		Igraph: IgraphConfig{
			MaxParallelCallsPerVersion: 1, // Conservative default; increase after validation
			MemoryLimitBytes:           0, // Unlimited by default
			EnableShim:                 true,
			FallbackOnError:            true,
		},
	}
}

// GetMaxParallelIgraphCalls returns the effective max parallel calls per version.
// Returns 1 if not configured or set to 0.
func (c *IgraphConfig) GetMaxParallelIgraphCalls() int {
	if c.MaxParallelCallsPerVersion <= 0 {
		return 1
	}
	return c.MaxParallelCallsPerVersion
}

// Load reads configuration from the specified file path.
// If the file doesn't exist, returns default configuration.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // Return defaults if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// GRPCAddr returns the gRPC server address string.
func (c *Config) GRPCAddr() string {
	return fmt.Sprintf(":%d", c.Server.GRPCPort)
}

// HTTPAddr returns the HTTP server address string.
func (c *Config) HTTPAddr() string {
	return fmt.Sprintf(":%d", c.Server.HTTPPort)
}

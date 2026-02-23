// Package service provides core services for the graph-engine.
package service

import (
	"compress/gzip"
	"encoding/gob"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func init() {
	// Register types for gob encoding
	gob.Register(&SnapshotData{})
	gob.Register(&GraphVersionSnapshot{})
	gob.Register(&AlgoResultSnapshot{})
	gob.Register(&ViewSnapshot{})
}

// PersistenceConfig holds configuration for the persistence layer.
type PersistenceConfig struct {
	// Enabled indicates whether persistence is enabled.
	Enabled bool

	// DataDir is the directory for snapshot files.
	DataDir string

	// SnapshotInterval is how often to auto-save snapshots.
	// 0 means no auto-save.
	SnapshotInterval time.Duration

	// MaxSnapshots is the maximum number of snapshots to retain.
	MaxSnapshots int

	// CompressSnapshots enables gzip compression for snapshots.
	CompressSnapshots bool
}

// DefaultPersistenceConfig returns default persistence configuration.
func DefaultPersistenceConfig() PersistenceConfig {
	return PersistenceConfig{
		Enabled:           false,
		DataDir:           "./data/snapshots",
		SnapshotInterval:  5 * time.Minute,
		MaxSnapshots:      5,
		CompressSnapshots: true,
	}
}

// SnapshotData holds the complete snapshot of service state.
type SnapshotData struct {
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`

	// Graph versions (metadata only, not full graph data)
	GraphVersions []GraphVersionSnapshot `json:"graph_versions"`

	// Algorithm results
	AlgoResults []AlgoResultSnapshot `json:"algo_results"`

	// Views
	Views []ViewSnapshot `json:"views"`
}

// GraphVersionSnapshot holds serializable graph version metadata.
type GraphVersionSnapshot struct {
	ID        string            `json:"id"`
	GraphName string            `json:"graph_name"`
	Directed  bool              `json:"directed"`
	VCount    uint64            `json:"vcount"`
	ECount    uint64            `json:"ecount"`
	Labels    map[string]string `json:"labels"`
	CreatedAt time.Time         `json:"created_at"`

	// Topology data for restoration
	NodeIDs     []uint64  `json:"node_ids"`
	EdgesSrcU64 []uint64  `json:"edges_src_u64"`
	EdgesDstU64 []uint64  `json:"edges_dst_u64"`
	EdgesWeight []float32 `json:"edges_weight,omitempty"`
}

// AlgoResultSnapshot holds serializable algorithm result.
type AlgoResultSnapshot struct {
	ID         string            `json:"id"`
	VersionID  string            `json:"version_id"`
	AlgoKind   string            `json:"algo_kind"`
	ParamsHash string            `json:"params_hash"`
	CreatedAt  time.Time         `json:"created_at"`
	Meta       map[string]string `json:"meta"`

	// Result data
	MembershipU32 []uint32  `json:"membership_u32,omitempty"`
	PathVertices  []uint64  `json:"path_vertices,omitempty"`
	PathEdges     []uint64  `json:"path_edges,omitempty"`
	PathCost      float64   `json:"path_cost,omitempty"`
	DistancesF64  []float64 `json:"distances_f64,omitempty"`
	CutValue      float64   `json:"cut_value,omitempty"`
	CutEdges      []uint64  `json:"cut_edges,omitempty"`
	KSPPaths      []KSPPath `json:"ksp_paths,omitempty"`
}

// ViewSnapshot holds serializable view data.
type ViewSnapshot struct {
	ID        string    `json:"id"`
	VersionID string    `json:"version_id"`
	SpecHash  string    `json:"spec_hash"`
	VCount    uint64    `json:"vcount"`
	ECount    uint64    `json:"ecount"`
	CreatedAt time.Time `json:"created_at"`

	// Vertex and edge sets for restoration
	VertexSet []uint64 `json:"vertex_set"`
}

// PersistenceManager handles snapshot export and import.
type PersistenceManager struct {
	config PersistenceConfig
	logger *slog.Logger

	// Services to snapshot
	versionStore *VersionStore
	resultStore  *ResultStore
	viewStore    *ViewStore

	// Auto-save control
	stopCh chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
}

// NewPersistenceManager creates a new persistence manager.
func NewPersistenceManager(
	config PersistenceConfig,
	logger *slog.Logger,
	versionStore *VersionStore,
	resultStore *ResultStore,
	viewStore *ViewStore,
) *PersistenceManager {
	pm := &PersistenceManager{
		config:       config,
		logger:       logger,
		versionStore: versionStore,
		resultStore:  resultStore,
		viewStore:    viewStore,
		stopCh:       make(chan struct{}),
	}

	// Create data directory if needed
	if config.Enabled {
		if err := os.MkdirAll(config.DataDir, 0755); err != nil {
			logger.Warn("failed to create snapshot directory", "error", err, "dir", config.DataDir)
		}
	}

	// Start auto-save if configured
	if config.Enabled && config.SnapshotInterval > 0 {
		pm.wg.Add(1)
		go pm.autoSaveLoop()
	}

	return pm
}

// autoSaveLoop periodically saves snapshots.
func (pm *PersistenceManager) autoSaveLoop() {
	defer pm.wg.Done()

	ticker := time.NewTicker(pm.config.SnapshotInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.stopCh:
			return
		case <-ticker.C:
			if err := pm.SaveSnapshot(); err != nil {
				pm.logger.Error("auto-save snapshot failed", "error", err)
			}
		}
	}
}

// SaveSnapshot creates a snapshot of current state.
func (pm *PersistenceManager) SaveSnapshot() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.config.Enabled {
		return nil
	}

	pm.logger.Info("creating snapshot")
	start := time.Now()

	// Build snapshot data
	snapshot := pm.buildSnapshot()

	// Generate filename with timestamp
	filename := fmt.Sprintf("snapshot_%s.gob", time.Now().Format("20060102_150405"))
	if pm.config.CompressSnapshots {
		filename += ".gz"
	}
	filepath := filepath.Join(pm.config.DataDir, filename)

	// Write snapshot
	if err := pm.writeSnapshot(filepath, snapshot); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	pm.logger.Info("snapshot created",
		"path", filepath,
		"duration", time.Since(start),
		"versions", len(snapshot.GraphVersions),
		"results", len(snapshot.AlgoResults),
		"views", len(snapshot.Views),
	)

	// Cleanup old snapshots
	pm.cleanupOldSnapshots()

	return nil
}

// buildSnapshot creates a SnapshotData from current service state.
func (pm *PersistenceManager) buildSnapshot() *SnapshotData {
	snapshot := &SnapshotData{
		Version:   "1.0",
		Timestamp: time.Now(),
	}

	// Snapshot graph versions
	if pm.versionStore != nil {
		pm.versionStore.mu.RLock()
		for _, entry := range pm.versionStore.versions {
			version := entry.version
			vs := GraphVersionSnapshot{
				ID:        version.ID,
				GraphName: version.GraphName,
				Directed:  version.Directed,
				VCount:    version.VCount,
				ECount:    version.ECount,
				Labels:    version.Labels,
				CreatedAt: version.PublishedAt,
			}

			// Include topology data - node IDs
			vs.NodeIDs = make([]uint64, version.VCount)
			for i := uint32(0); i < uint32(version.VCount); i++ {
				nodeID, ok := version.GetNodeID(i)
				if ok {
					vs.NodeIDs[i] = nodeID
				}
			}

			// Include edge data - convert compact indices to external node IDs
			vs.EdgesSrcU64 = make([]uint64, version.ECount)
			vs.EdgesDstU64 = make([]uint64, version.ECount)
			for i := uint64(0); i < version.ECount; i++ {
				srcIdx := version.EdgeSrc[i]
				dstIdx := version.EdgeDst[i]
				srcID, _ := version.GetNodeID(srcIdx)
				dstID, _ := version.GetNodeID(dstIdx)
				vs.EdgesSrcU64[i] = srcID
				vs.EdgesDstU64[i] = dstID
			}

			// Include weights if present
			if len(version.EdgeWeight) > 0 {
				vs.EdgesWeight = make([]float32, len(version.EdgeWeight))
				copy(vs.EdgesWeight, version.EdgeWeight)
			}

			snapshot.GraphVersions = append(snapshot.GraphVersions, vs)
		}
		pm.versionStore.mu.RUnlock()
	}

	// Snapshot algorithm results
	if pm.resultStore != nil {
		pm.resultStore.mu.RLock()
		for _, entry := range pm.resultStore.results {
			result := entry.result
			rs := AlgoResultSnapshot{
				ID:            result.ID,
				VersionID:     result.VersionID,
				AlgoKind:      string(result.AlgoKind),
				ParamsHash:    result.ParamsHash,
				CreatedAt:     result.CreatedAt,
				Meta:          result.Meta,
				MembershipU32: result.MembershipU32,
				PathVertices:  result.PathVertices,
				PathEdges:     result.PathEdges,
				PathCost:      result.PathCost,
				DistancesF64:  result.DistancesF64,
				CutValue:      result.CutValue,
				CutEdges:      result.CutEdges,
			}

			// Copy KSP paths
			if len(result.KSPPaths) > 0 {
				rs.KSPPaths = make([]KSPPath, len(result.KSPPaths))
				for i, p := range result.KSPPaths {
					rs.KSPPaths[i] = KSPPath{
						Vertices: p.Vertices,
						Edges:    p.Edges,
						Cost:     p.Cost,
					}
				}
			}

			snapshot.AlgoResults = append(snapshot.AlgoResults, rs)
		}
		pm.resultStore.mu.RUnlock()
	}

	// Snapshot views (just metadata, not full induced subgraph)
	if pm.viewStore != nil {
		pm.viewStore.mu.RLock()
		for _, entry := range pm.viewStore.views {
			view := entry.view
			vs := ViewSnapshot{
				ID:        view.ID,
				VersionID: view.VersionID,
				SpecHash:  view.SpecHash,
				VCount:    view.VCount,
				ECount:    view.ECount,
				CreatedAt: view.CreatedAt,
			}

			// Copy vertex mask indices (vertices included in view)
			if view.VertexMaskBitmap() != nil && view.version != nil {
				vs.VertexSet = make([]uint64, 0, view.VCount)
				it := view.VertexMaskBitmap().Iterator()
				for it.HasNext() {
					idx := it.Next()
					// Convert index to external node ID
					nodeID, ok := view.version.GetNodeID(idx)
					if ok {
						vs.VertexSet = append(vs.VertexSet, nodeID)
					}
				}
			}

			snapshot.Views = append(snapshot.Views, vs)
		}
		pm.viewStore.mu.RUnlock()
	}

	return snapshot
}

// writeSnapshot writes snapshot data to a file.
func (pm *PersistenceManager) writeSnapshot(path string, snapshot *SnapshotData) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	var writer io.Writer = file

	// Add compression if configured
	if pm.config.CompressSnapshots {
		gzWriter := gzip.NewWriter(file)
		defer func() { _ = gzWriter.Close() }()
		writer = gzWriter
	}

	// Encode with gob
	encoder := gob.NewEncoder(writer)
	return encoder.Encode(snapshot)
}

// LoadSnapshot loads the most recent snapshot.
func (pm *PersistenceManager) LoadSnapshot() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.config.Enabled {
		return nil
	}

	// Find most recent snapshot
	snapshotPath, err := pm.findLatestSnapshot()
	if err != nil {
		return fmt.Errorf("failed to find snapshot: %w", err)
	}
	if snapshotPath == "" {
		pm.logger.Info("no snapshot found to load")
		return nil
	}

	pm.logger.Info("loading snapshot", "path", snapshotPath)
	start := time.Now()

	// Read snapshot
	snapshot, err := pm.readSnapshot(snapshotPath)
	if err != nil {
		return fmt.Errorf("failed to read snapshot: %w", err)
	}

	// Restore state
	if err := pm.restoreSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to restore snapshot: %w", err)
	}

	pm.logger.Info("snapshot loaded",
		"path", snapshotPath,
		"duration", time.Since(start),
		"versions", len(snapshot.GraphVersions),
		"results", len(snapshot.AlgoResults),
		"views", len(snapshot.Views),
		"snapshot_time", snapshot.Timestamp,
	)

	return nil
}

// findLatestSnapshot finds the most recent snapshot file.
func (pm *PersistenceManager) findLatestSnapshot() (string, error) {
	entries, err := os.ReadDir(pm.config.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var latest string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if len(name) < 8 || name[:8] != "snapshot" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = filepath.Join(pm.config.DataDir, name)
		}
	}

	return latest, nil
}

// readSnapshot reads snapshot data from a file.
func (pm *PersistenceManager) readSnapshot(path string) (*SnapshotData, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var reader io.Reader = file

	// Detect and handle compression
	if filepath.Ext(path) == ".gz" {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer func() { _ = gzReader.Close() }()
		reader = gzReader
	}

	// Decode with gob
	var snapshot SnapshotData
	decoder := gob.NewDecoder(reader)
	if err := decoder.Decode(&snapshot); err != nil {
		return nil, err
	}

	return &snapshot, nil
}

// restoreSnapshot restores service state from a snapshot.
func (pm *PersistenceManager) restoreSnapshot(snapshot *SnapshotData) error {
	// Restore algorithm results
	if pm.resultStore != nil {
		for _, rs := range snapshot.AlgoResults {
			result := &AlgoResult{
				ID:            rs.ID,
				VersionID:     rs.VersionID,
				AlgoKind:      AlgoKind(rs.AlgoKind),
				ParamsHash:    rs.ParamsHash,
				CreatedAt:     rs.CreatedAt,
				Meta:          rs.Meta,
				MembershipU32: rs.MembershipU32,
				PathVertices:  rs.PathVertices,
				PathEdges:     rs.PathEdges,
				PathCost:      rs.PathCost,
				DistancesF64:  rs.DistancesF64,
				CutValue:      rs.CutValue,
				CutEdges:      rs.CutEdges,
				lastAccess:    time.Now(),
			}

			// Restore KSP paths
			if len(rs.KSPPaths) > 0 {
				result.KSPPaths = make([]*KSPPath, len(rs.KSPPaths))
				for i, p := range rs.KSPPaths {
					result.KSPPaths[i] = &KSPPath{
						Vertices: p.Vertices,
						Edges:    p.Edges,
						Cost:     p.Cost,
					}
				}
			}

			// Store result (may be evicted if at capacity)
			if err := pm.resultStore.Store(result); err != nil {
				pm.logger.Warn("failed to restore result",
					"result_id", result.ID,
					"error", err,
				)
			}
		}
	}

	// Note: Graph versions and views require their source data to restore properly.
	// For now, we only restore algorithm results which are cached computation outputs.
	// Full graph restoration would require re-building from source data.

	pm.logger.Info("snapshot restoration complete",
		"results_restored", len(snapshot.AlgoResults),
	)

	return nil
}

// cleanupOldSnapshots removes old snapshot files beyond MaxSnapshots.
func (pm *PersistenceManager) cleanupOldSnapshots() {
	if pm.config.MaxSnapshots <= 0 {
		return
	}

	entries, err := os.ReadDir(pm.config.DataDir)
	if err != nil {
		return
	}

	// Collect snapshot files with modification times
	type snapshotFile struct {
		path    string
		modTime time.Time
	}
	var snapshots []snapshotFile

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if len(name) < 8 || name[:8] != "snapshot" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		snapshots = append(snapshots, snapshotFile{
			path:    filepath.Join(pm.config.DataDir, name),
			modTime: info.ModTime(),
		})
	}

	// Sort by modification time (newest first)
	for i := 0; i < len(snapshots)-1; i++ {
		for j := i + 1; j < len(snapshots); j++ {
			if snapshots[j].modTime.After(snapshots[i].modTime) {
				snapshots[i], snapshots[j] = snapshots[j], snapshots[i]
			}
		}
	}

	// Remove old snapshots
	for i := pm.config.MaxSnapshots; i < len(snapshots); i++ {
		if err := os.Remove(snapshots[i].path); err != nil {
			pm.logger.Warn("failed to remove old snapshot", "path", snapshots[i].path, "error", err)
		} else {
			pm.logger.Debug("removed old snapshot", "path", snapshots[i].path)
		}
	}
}

// Stop stops the persistence manager.
func (pm *PersistenceManager) Stop() {
	close(pm.stopCh)
	pm.wg.Wait()

	// Save final snapshot before shutdown
	if pm.config.Enabled {
		if err := pm.SaveSnapshot(); err != nil {
			pm.logger.Error("failed to save final snapshot", "error", err)
		}
	}
}

// Stats returns persistence statistics.
func (pm *PersistenceManager) Stats() PersistenceStats {
	entries, _ := os.ReadDir(pm.config.DataDir)
	snapshotCount := 0
	var totalSize int64
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if len(entry.Name()) >= 8 && entry.Name()[:8] == "snapshot" {
			snapshotCount++
			if info, err := entry.Info(); err == nil {
				totalSize += info.Size()
				if info.ModTime().After(latestTime) {
					latestTime = info.ModTime()
				}
			}
		}
	}

	return PersistenceStats{
		Enabled:            pm.config.Enabled,
		SnapshotCount:      snapshotCount,
		TotalSizeBytes:     totalSize,
		LatestSnapshotTime: latestTime,
		DataDir:            pm.config.DataDir,
	}
}

// PersistenceStats holds persistence statistics.
type PersistenceStats struct {
	Enabled            bool
	SnapshotCount      int
	TotalSizeBytes     int64
	LatestSnapshotTime time.Time
	DataDir            string
}

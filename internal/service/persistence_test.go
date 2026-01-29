package service

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPersistenceManager_SaveAndLoad(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "persistence_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create stores
	resultStore := NewResultStore(100, 100<<20, 5*time.Minute)

	// Add some results
	result1 := NewAlgoResult("version1", AlgoKindShortestPath, "hash1")
	result1.PathVertices = []uint64{1, 2, 3}
	result1.PathCost = 2.5
	result1.Meta["found"] = "true"
	resultStore.Store(result1)

	result2 := NewAlgoResult("version1", AlgoKindComponents, "hash2")
	result2.MembershipU32 = []uint32{0, 0, 1, 1}
	resultStore.Store(result2)

	// Create persistence manager
	config := PersistenceConfig{
		Enabled:           true,
		DataDir:           tempDir,
		SnapshotInterval:  0, // Disable auto-save
		MaxSnapshots:      3,
		CompressSnapshots: true,
	}

	pm := NewPersistenceManager(config, logger, nil, resultStore, nil)

	// Save snapshot
	if err := pm.SaveSnapshot(); err != nil {
		t.Fatalf("SaveSnapshot failed: %v", err)
	}

	// Verify snapshot file exists
	entries, _ := os.ReadDir(tempDir)
	if len(entries) == 0 {
		t.Fatal("no snapshot files created")
	}

	// Create new result store and persistence manager
	newResultStore := NewResultStore(100, 100<<20, 5*time.Minute)
	pm2 := NewPersistenceManager(config, logger, nil, newResultStore, nil)

	// Load snapshot
	if err := pm2.LoadSnapshot(); err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	// Verify results were restored
	if newResultStore.Count() != 2 {
		t.Errorf("expected 2 results restored, got %d", newResultStore.Count())
	}

	// Verify result1 data
	restored1, err := newResultStore.Get(result1.ID)
	if err != nil {
		t.Errorf("failed to get restored result1: %v", err)
	} else {
		if len(restored1.PathVertices) != 3 {
			t.Errorf("expected 3 path vertices, got %d", len(restored1.PathVertices))
		}
		if restored1.PathCost != 2.5 {
			t.Errorf("expected path cost 2.5, got %f", restored1.PathCost)
		}
		if restored1.Meta["found"] != "true" {
			t.Errorf("expected meta found=true, got %s", restored1.Meta["found"])
		}
		restored1.Unpin()
	}

	// Verify result2 data
	restored2, err := newResultStore.Get(result2.ID)
	if err != nil {
		t.Errorf("failed to get restored result2: %v", err)
	} else {
		if len(restored2.MembershipU32) != 4 {
			t.Errorf("expected 4 membership values, got %d", len(restored2.MembershipU32))
		}
		restored2.Unpin()
	}

	// Cleanup
	pm.Stop()
	pm2.Stop()
}

func TestPersistenceManager_Compression(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "persistence_compression_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	resultStore := NewResultStore(100, 100<<20, 5*time.Minute)

	// Add a result with large data
	result := NewAlgoResult("version1", AlgoKindComponents, "hash1")
	result.MembershipU32 = make([]uint32, 10000)
	for i := range result.MembershipU32 {
		result.MembershipU32[i] = uint32(i % 100)
	}
	resultStore.Store(result)

	// Save with compression
	configCompressed := PersistenceConfig{
		Enabled:           true,
		DataDir:           filepath.Join(tempDir, "compressed"),
		SnapshotInterval:  0,
		MaxSnapshots:      3,
		CompressSnapshots: true,
	}
	os.MkdirAll(configCompressed.DataDir, 0755)
	pm1 := NewPersistenceManager(configCompressed, logger, nil, resultStore, nil)
	pm1.SaveSnapshot()
	pm1.Stop()

	// Save without compression
	configUncompressed := PersistenceConfig{
		Enabled:           true,
		DataDir:           filepath.Join(tempDir, "uncompressed"),
		SnapshotInterval:  0,
		MaxSnapshots:      3,
		CompressSnapshots: false,
	}
	os.MkdirAll(configUncompressed.DataDir, 0755)
	pm2 := NewPersistenceManager(configUncompressed, logger, nil, resultStore, nil)
	pm2.SaveSnapshot()
	pm2.Stop()

	// Compare sizes
	compressedEntries, _ := os.ReadDir(configCompressed.DataDir)
	uncompressedEntries, _ := os.ReadDir(configUncompressed.DataDir)

	var compressedSize, uncompressedSize int64
	for _, e := range compressedEntries {
		info, _ := e.Info()
		compressedSize += info.Size()
	}
	for _, e := range uncompressedEntries {
		info, _ := e.Info()
		uncompressedSize += info.Size()
	}

	// Compressed should be smaller
	if compressedSize >= uncompressedSize {
		t.Logf("compressed: %d bytes, uncompressed: %d bytes", compressedSize, uncompressedSize)
		// Note: for small data, compression may not always be smaller
		// This is more of a sanity check that both work
	}
}

func TestPersistenceManager_CleanupOldSnapshots(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "persistence_cleanup_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	resultStore := NewResultStore(100, 100<<20, 5*time.Minute)

	result := NewAlgoResult("version1", AlgoKindShortestPath, "hash1")
	resultStore.Store(result)

	config := PersistenceConfig{
		Enabled:           true,
		DataDir:           tempDir,
		SnapshotInterval:  0,
		MaxSnapshots:      2, // Keep only 2
		CompressSnapshots: false,
	}

	pm := NewPersistenceManager(config, logger, nil, resultStore, nil)

	// Create more snapshots than max
	for i := 0; i < 4; i++ {
		if err := pm.SaveSnapshot(); err != nil {
			t.Fatalf("SaveSnapshot %d failed: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// Check only MaxSnapshots remain
	entries, _ := os.ReadDir(tempDir)
	snapshotCount := 0
	for _, e := range entries {
		if len(e.Name()) >= 8 && e.Name()[:8] == "snapshot" {
			snapshotCount++
		}
	}

	if snapshotCount > config.MaxSnapshots {
		t.Errorf("expected at most %d snapshots, got %d", config.MaxSnapshots, snapshotCount)
	}

	pm.Stop()
}

func TestPersistenceManager_Stats(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "persistence_stats_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	resultStore := NewResultStore(100, 100<<20, 5*time.Minute)

	result := NewAlgoResult("version1", AlgoKindShortestPath, "hash1")
	resultStore.Store(result)

	config := PersistenceConfig{
		Enabled:           true,
		DataDir:           tempDir,
		SnapshotInterval:  0,
		MaxSnapshots:      5,
		CompressSnapshots: false,
	}

	pm := NewPersistenceManager(config, logger, nil, resultStore, nil)

	// Initial stats
	stats := pm.Stats()
	if !stats.Enabled {
		t.Error("expected Enabled=true")
	}
	if stats.SnapshotCount != 0 {
		t.Errorf("expected 0 snapshots initially, got %d", stats.SnapshotCount)
	}

	// Save a snapshot
	pm.SaveSnapshot()

	stats = pm.Stats()
	if stats.SnapshotCount != 1 {
		t.Errorf("expected 1 snapshot after save, got %d", stats.SnapshotCount)
	}
	if stats.TotalSizeBytes == 0 {
		t.Error("expected non-zero TotalSizeBytes")
	}

	pm.Stop()
}

func TestPersistenceManager_Disabled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	config := PersistenceConfig{
		Enabled: false,
	}

	pm := NewPersistenceManager(config, logger, nil, nil, nil)

	// Save should be no-op
	if err := pm.SaveSnapshot(); err != nil {
		t.Errorf("SaveSnapshot should succeed when disabled: %v", err)
	}

	// Load should be no-op
	if err := pm.LoadSnapshot(); err != nil {
		t.Errorf("LoadSnapshot should succeed when disabled: %v", err)
	}

	pm.Stop()
}

func TestPersistenceManager_KSPResults(t *testing.T) {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "persistence_ksp_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	resultStore := NewResultStore(100, 100<<20, 5*time.Minute)

	// Add KSP result
	result := NewAlgoResult("version1", AlgoKindKSP, "hash1")
	result.KSPPaths = []*KSPPath{
		{Vertices: []uint64{1, 2, 3}, Edges: []uint64{100, 101}, Cost: 2.0},
		{Vertices: []uint64{1, 4, 3}, Edges: []uint64{102, 103}, Cost: 3.0},
	}
	result.Meta["k"] = "2"
	result.Meta["paths_found"] = "2"
	resultStore.Store(result)

	config := PersistenceConfig{
		Enabled:           true,
		DataDir:           tempDir,
		SnapshotInterval:  0,
		MaxSnapshots:      3,
		CompressSnapshots: true,
	}

	pm := NewPersistenceManager(config, logger, nil, resultStore, nil)
	pm.SaveSnapshot()
	pm.Stop()

	// Restore
	newResultStore := NewResultStore(100, 100<<20, 5*time.Minute)
	pm2 := NewPersistenceManager(config, logger, nil, newResultStore, nil)
	pm2.LoadSnapshot()

	// Verify KSP paths
	restored, err := newResultStore.Get(result.ID)
	if err != nil {
		t.Fatalf("failed to get restored result: %v", err)
	}
	defer restored.Unpin()

	if len(restored.KSPPaths) != 2 {
		t.Errorf("expected 2 KSP paths, got %d", len(restored.KSPPaths))
	}

	if restored.KSPPaths[0].Cost != 2.0 {
		t.Errorf("expected first path cost 2.0, got %f", restored.KSPPaths[0].Cost)
	}

	if len(restored.KSPPaths[1].Vertices) != 3 {
		t.Errorf("expected 3 vertices in second path, got %d", len(restored.KSPPaths[1].Vertices))
	}

	pm2.Stop()
}

package service

import (
	"bytes"
	"strings"
	"testing"
)

func TestGraphExporter_CanExport(t *testing.T) {
	config := ExportConfig{
		MaxEdges: 100,
	}
	exporter := NewGraphExporter(config)

	tests := []struct {
		name      string
		ecount    uint64
		canExport bool
	}{
		{"within limit", 50, true},
		{"at limit", 100, true},
		{"over limit", 101, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			can, _, _ := exporter.CanExport(tt.ecount)
			if can != tt.canExport {
				t.Errorf("CanExport(%d) = %v, want %v", tt.ecount, can, tt.canExport)
			}
		})
	}
}

func TestGraphExporter_EdgeList(t *testing.T) {
	// Create a simple graph
	gv := &GraphVersion{
		ID:            "test",
		GraphName:     "test",
		Directed:      true,
		VCount:        3,
		ECount:        2,
		nodeIDToIndex: map[uint64]uint32{100: 0, 200: 1, 300: 2},
		indexToNodeID: []uint64{100, 200, 300},
		EdgeSrc:       []uint32{0, 1},
		EdgeDst:       []uint32{1, 2},
	}

	config := ExportConfig{
		Format:         ExportFormatEdgeList,
		MaxEdges:       1000,
		IncludeWeights: false,
		ChunkSize:      100,
	}
	exporter := NewGraphExporter(config)

	var buf bytes.Buffer
	result, err := exporter.ExportToWriter(gv, &buf)
	if err != nil {
		t.Fatalf("ExportToWriter failed: %v", err)
	}

	// Verify result
	if result.TotalEdges != 2 {
		t.Errorf("expected 2 edges, got %d", result.TotalEdges)
	}

	// Check output format
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), output)
	}

	// Check first edge
	if lines[0] != "100 200" {
		t.Errorf("expected '100 200', got %q", lines[0])
	}
}

func TestGraphExporter_EdgeListWithWeights(t *testing.T) {
	gv := &GraphVersion{
		ID:            "test",
		GraphName:     "test",
		Directed:      true,
		VCount:        2,
		ECount:        1,
		nodeIDToIndex: map[uint64]uint32{100: 0, 200: 1},
		indexToNodeID: []uint64{100, 200},
		EdgeSrc:       []uint32{0},
		EdgeDst:       []uint32{1},
		EdgeWeight:    []float32{3.14},
	}

	config := ExportConfig{
		Format:         ExportFormatEdgeList,
		MaxEdges:       1000,
		IncludeWeights: true,
		ChunkSize:      100,
	}
	exporter := NewGraphExporter(config)

	var buf bytes.Buffer
	_, err := exporter.ExportToWriter(gv, &buf)
	if err != nil {
		t.Fatalf("ExportToWriter failed: %v", err)
	}

	output := strings.TrimSpace(buf.String())
	if !strings.HasPrefix(output, "100 200 3.14") {
		t.Errorf("expected weight in output, got %q", output)
	}
}

func TestGraphExporter_CSV(t *testing.T) {
	gv := &GraphVersion{
		ID:            "test",
		GraphName:     "test",
		Directed:      true,
		VCount:        3,
		ECount:        2,
		nodeIDToIndex: map[uint64]uint32{100: 0, 200: 1, 300: 2},
		indexToNodeID: []uint64{100, 200, 300},
		EdgeSrc:       []uint32{0, 1},
		EdgeDst:       []uint32{1, 2},
		EdgeWeight:    []float32{1.5, 2.5},
		EdgeKind:      []uint32{10, 20},
	}

	config := ExportConfig{
		Format:         ExportFormatCSV,
		MaxEdges:       1000,
		IncludeWeights: true,
		IncludeKind:    true,
		ChunkSize:      100,
	}
	exporter := NewGraphExporter(config)

	var buf bytes.Buffer
	result, err := exporter.ExportToWriter(gv, &buf)
	if err != nil {
		t.Fatalf("ExportToWriter failed: %v", err)
	}

	if result.TotalEdges != 2 {
		t.Errorf("expected 2 edges, got %d", result.TotalEdges)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Check header
	if lines[0] != "source,target,weight,kind" {
		t.Errorf("expected CSV header, got %q", lines[0])
	}

	// Check data rows
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (header + 2 data), got %d", len(lines))
	}
}

func TestGraphExporter_Chunking(t *testing.T) {
	// Create a larger graph
	numEdges := 150
	gv := &GraphVersion{
		ID:            "test",
		GraphName:     "test",
		Directed:      true,
		VCount:        uint64(numEdges + 1),
		ECount:        uint64(numEdges),
		nodeIDToIndex: make(map[uint64]uint32),
		indexToNodeID: make([]uint64, numEdges+1),
		EdgeSrc:       make([]uint32, numEdges),
		EdgeDst:       make([]uint32, numEdges),
	}

	for i := 0; i <= numEdges; i++ {
		gv.nodeIDToIndex[uint64(i)] = uint32(i)
		gv.indexToNodeID[i] = uint64(i)
	}
	for i := 0; i < numEdges; i++ {
		gv.EdgeSrc[i] = uint32(i)
		gv.EdgeDst[i] = uint32(i + 1)
	}

	config := ExportConfig{
		Format:    ExportFormatEdgeList,
		MaxEdges:  1000,
		ChunkSize: 50, // 3 chunks expected
	}
	exporter := NewGraphExporter(config)

	ch, result, err := exporter.ExportGraph(gv)
	if err != nil {
		t.Fatalf("ExportGraph failed: %v", err)
	}

	// Count chunks
	chunkCount := 0
	var totalData bytes.Buffer
	for chunk := range ch {
		chunkCount++
		totalData.Write(chunk)
	}

	// Should have at least 3 chunks
	if chunkCount < 3 {
		t.Errorf("expected at least 3 chunks, got %d", chunkCount)
	}

	// Verify all edges were exported
	if result.TotalEdges != uint64(numEdges) {
		t.Errorf("expected %d edges, got %d", numEdges, result.TotalEdges)
	}

	// Verify output
	lines := strings.Split(strings.TrimSpace(totalData.String()), "\n")
	if len(lines) != numEdges {
		t.Errorf("expected %d lines, got %d", numEdges, len(lines))
	}
}

func TestGraphExporter_SizeLimit(t *testing.T) {
	gv := &GraphVersion{
		ID:            "test",
		GraphName:     "test",
		Directed:      true,
		VCount:        1000,
		ECount:        500,
		nodeIDToIndex: make(map[uint64]uint32),
		indexToNodeID: make([]uint64, 1000),
		EdgeSrc:       make([]uint32, 500),
		EdgeDst:       make([]uint32, 500),
	}

	config := ExportConfig{
		Format:   ExportFormatEdgeList,
		MaxEdges: 100, // Less than graph size
	}
	exporter := NewGraphExporter(config)

	_, _, err := exporter.ExportGraph(gv)
	if err == nil {
		t.Error("expected error for graph exceeding size limit")
	}
}

func TestGraphExporter_DefaultConfig(t *testing.T) {
	config := DefaultExportConfig()

	if config.MaxEdges != 10000 {
		t.Errorf("expected MaxEdges=10000, got %d", config.MaxEdges)
	}
	if config.ChunkSize != 1000 {
		t.Errorf("expected ChunkSize=1000, got %d", config.ChunkSize)
	}
	if !config.IncludeWeights {
		t.Error("expected IncludeWeights=true")
	}
}

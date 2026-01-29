// Package service provides core services for the graph-engine.
package service

import (
	"bytes"
	"fmt"
	"io"
)

// ExportFormat represents the output format for graph export.
type ExportFormat int

const (
	// ExportFormatEdgeList exports as "src_id dst_id [weight]" per line
	ExportFormatEdgeList ExportFormat = iota
	// ExportFormatCSV exports as CSV with headers
	ExportFormatCSV
)

// ExportConfig holds configuration for graph export.
type ExportConfig struct {
	// Format is the output format
	Format ExportFormat

	// MaxEdges is the maximum number of edges to export (0 = use default)
	MaxEdges uint64

	// IncludeWeights includes edge weights in output
	IncludeWeights bool

	// IncludeKind includes edge kind/type in output
	IncludeKind bool

	// ChunkSize is the number of edges per chunk (for streaming)
	ChunkSize int
}

// DefaultExportConfig returns default export configuration.
func DefaultExportConfig() ExportConfig {
	return ExportConfig{
		Format:         ExportFormatEdgeList,
		MaxEdges:       10000,
		IncludeWeights: true,
		IncludeKind:    true,
		ChunkSize:      1000,
	}
}

// ExportResult holds export operation results.
type ExportResult struct {
	// TotalEdges is the number of edges exported
	TotalEdges uint64

	// TotalBytes is the total size of exported data
	TotalBytes uint64

	// Truncated is true if export was limited by MaxEdges
	Truncated bool
}

// GraphExporter exports graph data in various formats.
type GraphExporter struct {
	config ExportConfig
}

// NewGraphExporter creates a new graph exporter.
func NewGraphExporter(config ExportConfig) *GraphExporter {
	if config.ChunkSize <= 0 {
		config.ChunkSize = 1000
	}
	if config.MaxEdges == 0 {
		config.MaxEdges = 10000
	}
	return &GraphExporter{config: config}
}

// CanExport checks if the graph/view can be exported within size limits.
// Returns (canExport, edgeCount, error message).
func (e *GraphExporter) CanExport(ecount uint64) (bool, uint64, string) {
	if ecount > e.config.MaxEdges {
		return false, ecount, fmt.Sprintf(
			"graph has %d edges, exceeds limit of %d. Use a smaller view.",
			ecount, e.config.MaxEdges)
	}
	return true, ecount, ""
}

// ExportGraph exports a graph version to the specified format.
// Returns chunks of data for streaming.
func (e *GraphExporter) ExportGraph(gv *GraphVersion) (<-chan []byte, *ExportResult, error) {
	canExport, ecount, msg := e.CanExport(gv.ECount)
	if !canExport {
		return nil, nil, fmt.Errorf("%s", msg)
	}

	result := &ExportResult{
		TotalEdges: ecount,
		Truncated:  false,
	}

	ch := make(chan []byte, 10)

	go func() {
		defer close(ch)

		switch e.config.Format {
		case ExportFormatEdgeList:
			e.exportEdgeList(gv, ch, result)
		case ExportFormatCSV:
			e.exportCSV(gv, ch, result)
		}
	}()

	return ch, result, nil
}

// ExportView exports a view to the specified format.
func (e *GraphExporter) ExportView(view *View) (<-chan []byte, *ExportResult, error) {
	canExport, ecount, msg := e.CanExport(view.ECount)
	if !canExport {
		return nil, nil, fmt.Errorf("%s", msg)
	}

	result := &ExportResult{
		TotalEdges: ecount,
		Truncated:  false,
	}

	ch := make(chan []byte, 10)

	go func() {
		defer close(ch)

		switch e.config.Format {
		case ExportFormatEdgeList:
			e.exportViewEdgeList(view, ch, result)
		case ExportFormatCSV:
			e.exportViewCSV(view, ch, result)
		}
	}()

	return ch, result, nil
}

// exportEdgeList exports graph in edge list format.
func (e *GraphExporter) exportEdgeList(gv *GraphVersion, ch chan<- []byte, result *ExportResult) {
	var buf bytes.Buffer
	edgesInChunk := 0

	for i := uint64(0); i < gv.ECount; i++ {
		srcIdx := gv.EdgeSrc[i]
		dstIdx := gv.EdgeDst[i]

		// Get external node IDs
		srcID, _ := gv.GetNodeID(srcIdx)
		dstID, _ := gv.GetNodeID(dstIdx)

		// Write edge
		if e.config.IncludeWeights && gv.EdgeWeight != nil {
			fmt.Fprintf(&buf, "%d %d %.6f\n", srcID, dstID, gv.EdgeWeight[i])
		} else {
			fmt.Fprintf(&buf, "%d %d\n", srcID, dstID)
		}

		edgesInChunk++

		// Send chunk if full
		if edgesInChunk >= e.config.ChunkSize {
			data := make([]byte, buf.Len())
			copy(data, buf.Bytes())
			ch <- data
			result.TotalBytes += uint64(len(data))
			buf.Reset()
			edgesInChunk = 0
		}
	}

	// Send remaining data
	if buf.Len() > 0 {
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
		ch <- data
		result.TotalBytes += uint64(len(data))
	}
}

// exportCSV exports graph in CSV format.
func (e *GraphExporter) exportCSV(gv *GraphVersion, ch chan<- []byte, result *ExportResult) {
	var buf bytes.Buffer
	edgesInChunk := 0

	// Write header
	if e.config.IncludeWeights && e.config.IncludeKind {
		buf.WriteString("source,target,weight,kind\n")
	} else if e.config.IncludeWeights {
		buf.WriteString("source,target,weight\n")
	} else if e.config.IncludeKind {
		buf.WriteString("source,target,kind\n")
	} else {
		buf.WriteString("source,target\n")
	}

	for i := uint64(0); i < gv.ECount; i++ {
		srcIdx := gv.EdgeSrc[i]
		dstIdx := gv.EdgeDst[i]

		// Get external node IDs
		srcID, _ := gv.GetNodeID(srcIdx)
		dstID, _ := gv.GetNodeID(dstIdx)

		// Write edge
		if e.config.IncludeWeights && e.config.IncludeKind && gv.EdgeWeight != nil && gv.EdgeKind != nil {
			fmt.Fprintf(&buf, "%d,%d,%.6f,%d\n", srcID, dstID, gv.EdgeWeight[i], gv.EdgeKind[i])
		} else if e.config.IncludeWeights && gv.EdgeWeight != nil {
			fmt.Fprintf(&buf, "%d,%d,%.6f\n", srcID, dstID, gv.EdgeWeight[i])
		} else if e.config.IncludeKind && gv.EdgeKind != nil {
			fmt.Fprintf(&buf, "%d,%d,%d\n", srcID, dstID, gv.EdgeKind[i])
		} else {
			fmt.Fprintf(&buf, "%d,%d\n", srcID, dstID)
		}

		edgesInChunk++

		// Send chunk if full
		if edgesInChunk >= e.config.ChunkSize {
			data := make([]byte, buf.Len())
			copy(data, buf.Bytes())
			ch <- data
			result.TotalBytes += uint64(len(data))
			buf.Reset()
			edgesInChunk = 0
		}
	}

	// Send remaining data
	if buf.Len() > 0 {
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
		ch <- data
		result.TotalBytes += uint64(len(data))
	}
}

// exportViewEdgeList exports view in edge list format.
func (e *GraphExporter) exportViewEdgeList(view *View, ch chan<- []byte, result *ExportResult) {
	var buf bytes.Buffer
	edgesInChunk := 0
	gv := view.version

	for i := uint64(0); i < gv.ECount; i++ {
		// Skip edges not in view
		if view != nil && !view.ContainsEdge(int(i)) {
			continue
		}

		srcIdx := gv.EdgeSrc[i]
		dstIdx := gv.EdgeDst[i]

		// Get external node IDs
		srcID, _ := gv.GetNodeID(srcIdx)
		dstID, _ := gv.GetNodeID(dstIdx)

		// Write edge
		if e.config.IncludeWeights && gv.EdgeWeight != nil {
			fmt.Fprintf(&buf, "%d %d %.6f\n", srcID, dstID, gv.EdgeWeight[i])
		} else {
			fmt.Fprintf(&buf, "%d %d\n", srcID, dstID)
		}

		edgesInChunk++

		// Send chunk if full
		if edgesInChunk >= e.config.ChunkSize {
			data := make([]byte, buf.Len())
			copy(data, buf.Bytes())
			ch <- data
			result.TotalBytes += uint64(len(data))
			buf.Reset()
			edgesInChunk = 0
		}
	}

	// Send remaining data
	if buf.Len() > 0 {
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
		ch <- data
		result.TotalBytes += uint64(len(data))
	}
}

// exportViewCSV exports view in CSV format.
func (e *GraphExporter) exportViewCSV(view *View, ch chan<- []byte, result *ExportResult) {
	var buf bytes.Buffer
	edgesInChunk := 0
	gv := view.version

	// Write header
	if e.config.IncludeWeights && e.config.IncludeKind {
		buf.WriteString("source,target,weight,kind\n")
	} else if e.config.IncludeWeights {
		buf.WriteString("source,target,weight\n")
	} else if e.config.IncludeKind {
		buf.WriteString("source,target,kind\n")
	} else {
		buf.WriteString("source,target\n")
	}

	for i := uint64(0); i < gv.ECount; i++ {
		// Skip edges not in view
		if view != nil && !view.ContainsEdge(int(i)) {
			continue
		}

		srcIdx := gv.EdgeSrc[i]
		dstIdx := gv.EdgeDst[i]

		// Get external node IDs
		srcID, _ := gv.GetNodeID(srcIdx)
		dstID, _ := gv.GetNodeID(dstIdx)

		// Write edge
		if e.config.IncludeWeights && e.config.IncludeKind && gv.EdgeWeight != nil && gv.EdgeKind != nil {
			fmt.Fprintf(&buf, "%d,%d,%.6f,%d\n", srcID, dstID, gv.EdgeWeight[i], gv.EdgeKind[i])
		} else if e.config.IncludeWeights && gv.EdgeWeight != nil {
			fmt.Fprintf(&buf, "%d,%d,%.6f\n", srcID, dstID, gv.EdgeWeight[i])
		} else if e.config.IncludeKind && gv.EdgeKind != nil {
			fmt.Fprintf(&buf, "%d,%d,%d\n", srcID, dstID, gv.EdgeKind[i])
		} else {
			fmt.Fprintf(&buf, "%d,%d\n", srcID, dstID)
		}

		edgesInChunk++

		// Send chunk if full
		if edgesInChunk >= e.config.ChunkSize {
			data := make([]byte, buf.Len())
			copy(data, buf.Bytes())
			ch <- data
			result.TotalBytes += uint64(len(data))
			buf.Reset()
			edgesInChunk = 0
		}
	}

	// Send remaining data
	if buf.Len() > 0 {
		data := make([]byte, buf.Len())
		copy(data, buf.Bytes())
		ch <- data
		result.TotalBytes += uint64(len(data))
	}
}

// ExportToWriter exports graph data to an io.Writer (for non-streaming use).
func (e *GraphExporter) ExportToWriter(gv *GraphVersion, w io.Writer) (*ExportResult, error) {
	ch, result, err := e.ExportGraph(gv)
	if err != nil {
		return nil, err
	}

	for chunk := range ch {
		if _, err := w.Write(chunk); err != nil {
			return result, err
		}
	}

	return result, nil
}

// ExportViewToWriter exports view data to an io.Writer.
func (e *GraphExporter) ExportViewToWriter(view *View, w io.Writer) (*ExportResult, error) {
	ch, result, err := e.ExportView(view)
	if err != nil {
		return nil, err
	}

	for chunk := range ch {
		if _, err := w.Write(chunk); err != nil {
			return result, err
		}
	}

	return result, nil
}

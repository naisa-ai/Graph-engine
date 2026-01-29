package service

import (
	"testing"
	"time"
)

func TestTraceSpan_Basic(t *testing.T) {
	span := NewTraceSpan("test-operation")

	// Verify initial state
	if span.Name != "test-operation" {
		t.Errorf("expected name 'test-operation', got %s", span.Name)
	}
	if span.StartTime.IsZero() {
		t.Error("expected non-zero start time")
	}
	if span.Duration != 0 {
		t.Errorf("expected zero duration before End(), got %v", span.Duration)
	}

	// Simulate work
	time.Sleep(10 * time.Millisecond)

	// End the span
	span.End()

	// Verify duration
	if span.Duration < 10*time.Millisecond {
		t.Errorf("expected duration >= 10ms, got %v", span.Duration)
	}
}

func TestTraceSpan_Tags(t *testing.T) {
	span := NewTraceSpan("tagged-operation")
	span.AddTag("key1", "value1")
	span.AddTag("key2", "value2")

	if span.Tags["key1"] != "value1" {
		t.Errorf("expected tag key1=value1, got %s", span.Tags["key1"])
	}
	if span.Tags["key2"] != "value2" {
		t.Errorf("expected tag key2=value2, got %s", span.Tags["key2"])
	}
}

func TestTraceSpan_Chaining(t *testing.T) {
	span := NewTraceSpan("chained")
	span.AddTag("a", "1").AddTag("b", "2").AddTag("c", "3")

	if len(span.Tags) != 3 {
		t.Errorf("expected 3 tags, got %d", len(span.Tags))
	}
}

func TestAlgoResult_Spans(t *testing.T) {
	result := NewAlgoResult("version1", AlgoKindShortestPath, "hash1")

	// Add spans
	span1 := result.StartSpan("cache_lookup")
	time.Sleep(5 * time.Millisecond)
	span1.End()
	span1.AddTag("hit", "false")
	result.AddSpan(span1)

	span2 := result.StartSpan("compute")
	time.Sleep(10 * time.Millisecond)
	span2.End()
	result.AddSpan(span2)

	// Verify spans
	if len(result.Spans) != 2 {
		t.Errorf("expected 2 spans, got %d", len(result.Spans))
	}

	if result.Spans[0].Name != "cache_lookup" {
		t.Errorf("expected first span 'cache_lookup', got %s", result.Spans[0].Name)
	}

	if result.Spans[0].Tags["hit"] != "false" {
		t.Errorf("expected tag hit=false, got %s", result.Spans[0].Tags["hit"])
	}
}

func TestAlgoResult_RecordSpan(t *testing.T) {
	result := NewAlgoResult("version1", AlgoKindComponents, "hash1")

	// Record a span with known duration
	result.RecordSpan("already_timed", 100*time.Millisecond, map[string]string{
		"operation": "compute",
	})

	if len(result.Spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(result.Spans))
	}

	span := result.Spans[0]
	if span.Name != "already_timed" {
		t.Errorf("expected name 'already_timed', got %s", span.Name)
	}
	if span.Duration != 100*time.Millisecond {
		t.Errorf("expected duration 100ms, got %v", span.Duration)
	}
	if span.Tags["operation"] != "compute" {
		t.Errorf("expected tag operation=compute, got %s", span.Tags["operation"])
	}
}

func TestAlgoResult_SpansInitialized(t *testing.T) {
	result := NewAlgoResult("version1", AlgoKindDistances, "hash1")

	// Spans should be initialized to empty slice, not nil
	if result.Spans == nil {
		t.Error("expected Spans to be initialized, got nil")
	}
	if len(result.Spans) != 0 {
		t.Errorf("expected 0 spans initially, got %d", len(result.Spans))
	}
}

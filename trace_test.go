package nand

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestEstimateCost(t *testing.T) {
	usage := Usage{InputTokens: 500_000, OutputTokens: 250_000, TotalTokens: 750_000}
	if got := EstimateCost(usage, Pricing{}); got != 0 {
		t.Fatalf("got %f, want 0", got)
	}

	pricing := Pricing{InputPerMillion: 0.10, OutputPerMillion: 0.40}
	got := EstimateCost(usage, pricing)
	want := 0.15
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %f, want %f", got, want)
	}
}

func TestJSONLTracerWritesLines(t *testing.T) {
	var buf bytes.Buffer
	tracer := NewJSONLTracer(&buf)

	allowed := true
	err := tracer.WriteEvent(TraceEvent{
		Timestamp: time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
		Type:      "tool_call",
		Turn:      1,
		ToolName:  "read",
		Allowed:   &allowed,
		Truncated: true,
	})
	if err != nil {
		t.Fatalf("WriteEvent error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}

	var decoded TraceEvent
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("unmarshal trace line: %v", err)
	}
	if decoded.Type != "tool_call" || decoded.ToolName != "read" || !decoded.Truncated {
		t.Errorf("unexpected decoded event: %+v", decoded)
	}
	if decoded.Allowed == nil || !*decoded.Allowed {
		t.Errorf("expected allowed=true, got %v", decoded.Allowed)
	}
}

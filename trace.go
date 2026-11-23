package nand

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

type Pricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

func EstimateCost(usage Usage, pricing Pricing) float64 {
	inCost := float64(usage.InputTokens) * pricing.InputPerMillion / 1_000_000
	outCost := float64(usage.OutputTokens) * pricing.OutputPerMillion / 1_000_000
	return inCost + outCost
}

type TraceEvent struct {
	Timestamp    time.Time       `json:"timestamp"`
	Type         string          `json:"type"`
	Turn         int             `json:"turn,omitempty"`
	DurationMS   int64           `json:"duration_ms,omitempty"`
	Usage        *Usage          `json:"usage,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	ToolName     string          `json:"tool_name,omitempty"`
	Permission   PermissionLevel `json:"permission,omitempty"`
	Allowed      *bool           `json:"allowed,omitempty"`
	Truncated    bool            `json:"truncated,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
	Detail       string          `json:"detail,omitempty"`
}

type Tracer interface {
	WriteEvent(event TraceEvent) error
}

type JSONLTracer struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func NewJSONLTracer(w io.Writer) *JSONLTracer {
	return &JSONLTracer{enc: json.NewEncoder(w)}
}

func (t *JSONLTracer) WriteEvent(event TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enc.Encode(event)
}

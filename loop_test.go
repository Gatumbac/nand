package nand

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

type fakeProvider struct {
	responses []CompletionResponse
	requests  []CompletionRequest
}

func (f *fakeProvider) Complete(_ context.Context, req CompletionRequest) (CompletionResponse, error) {
	copied := make([]Message, len(req.Messages))
	copy(copied, req.Messages)
	req.Messages = copied
	f.requests = append(f.requests, req)

	idx := len(f.requests) - 1
	if idx >= len(f.responses) {
		return CompletionResponse{}, errors.New("unexpected extra provider call")
	}
	return f.responses[idx], nil
}

type fakeTool struct {
	name       string
	permission PermissionLevel
	runFn      func(ctx context.Context, args json.RawMessage) (string, error)
}

func (t *fakeTool) Name() string                { return t.name }
func (t *fakeTool) Description() string         { return "fake tool" }
func (t *fakeTool) Schema() json.RawMessage     { return json.RawMessage(`{"type":"object"}`) }
func (t *fakeTool) Permission() PermissionLevel { return t.permission }
func (t *fakeTool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.runFn(ctx, args)
}

func TestNewRegistryValidation(t *testing.T) {
	valid := &fakeTool{name: "read", permission: PermissionRead}

	if _, err := NewRegistry(nil); err == nil {
		t.Fatal("expected error for nil tool")
	}
	if _, err := NewRegistry(&fakeTool{name: "   "}); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := NewRegistry(valid, valid); err == nil {
		t.Fatal("expected error for duplicate tool name")
	}
}

func TestAgentSingleTurnReply(t *testing.T) {
	provider := &fakeProvider{
		responses: []CompletionResponse{
			{
				Message: Message{Role: RoleAssistant, Content: "hello"},
				Usage:   Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			},
		},
	}

	agent, err := NewAgent(AgentConfig{Provider: provider, System: "Be concise."})
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	res, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if res.Reply != "hello" {
		t.Errorf("got reply %q, want %q", res.Reply, "hello")
	}
	if res.Turns != 1 {
		t.Errorf("got %d turns, want 1", res.Turns)
	}
	if res.Usage != (Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}) {
		t.Errorf("unexpected usage: %+v", res.Usage)
	}
}

func TestAgentMultipleToolCallsAndPreservesOpaqueData(t *testing.T) {
	rawCallA := json.RawMessage(`{"id":"call_a","type":"function","function":{"name":"echo","arguments":"{\"v\":\"A\"}"},"extra_content":{"sig":"a"}}`)
	rawCallB := json.RawMessage(`{"id":"call_b","type":"function","function":{"name":"echo","arguments":"{\"v\":\"B\"}"},"extra_content":{"sig":"b"}}`)
	opaque := map[string]json.RawMessage{"extra_content": json.RawMessage(`{"sig":"msg"}`)}

	provider := &fakeProvider{
		responses: []CompletionResponse{
			{
				Message: Message{
					Role:         RoleAssistant,
					ProviderData: opaque,
					ToolCalls: []ToolCall{
						{ID: "call_a", Name: "echo", Arguments: json.RawMessage(`{"v":"A"}`), Raw: rawCallA},
						{ID: "call_b", Name: "echo", Arguments: json.RawMessage(`{"v":"B"}`), Raw: rawCallB},
					},
				},
				Usage: Usage{InputTokens: 20, OutputTokens: 10, TotalTokens: 30},
			},
			{
				Message: Message{Role: RoleAssistant, Content: "both done"},
				Usage:   Usage{InputTokens: 40, OutputTokens: 6, TotalTokens: 46},
			},
		},
	}

	var seen []string
	echoTool := &fakeTool{
		name:       "echo",
		permission: PermissionRead,
		runFn: func(_ context.Context, args json.RawMessage) (string, error) {
			seen = append(seen, string(args))
			return "out:" + string(args), nil
		},
	}

	reg, err := NewRegistry(echoTool)
	if err != nil {
		t.Fatalf("NewRegistry error: %v", err)
	}

	agent, err := NewAgent(AgentConfig{Provider: provider, Registry: reg, MaxTurns: 5})
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	res, err := agent.Run(context.Background(), "run both")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if res.Reply != "both done" || res.Turns != 2 {
		t.Fatalf("got reply=%q turns=%d, want reply=%q turns=2", res.Reply, res.Turns, "both done")
	}
	if !reflect.DeepEqual(seen, []string{`{"v":"A"}`, `{"v":"B"}`}) {
		t.Errorf("got tool args %v", seen)
	}
	if res.Usage != (Usage{InputTokens: 60, OutputTokens: 16, TotalTokens: 76}) {
		t.Errorf("unexpected total usage: %+v", res.Usage)
	}

	secondReq := provider.requests[1]
	if len(secondReq.Messages) != 4 {
		t.Fatalf("got %d messages on turn 2, want 4", len(secondReq.Messages))
	}
	assistantMsg := secondReq.Messages[1]
	if string(assistantMsg.ProviderData["extra_content"]) != `{"sig":"msg"}` {
		t.Errorf("provider data not preserved: %v", assistantMsg.ProviderData)
	}
	if string(assistantMsg.ToolCalls[0].Raw) != string(rawCallA) || string(assistantMsg.ToolCalls[1].Raw) != string(rawCallB) {
		t.Errorf("raw tool_calls were modified")
	}
	if secondReq.Messages[2].ToolCallID != "call_a" || secondReq.Messages[3].ToolCallID != "call_b" {
		t.Errorf("unexpected tool result order: %+v", secondReq.Messages[2:])
	}
}

func TestAgentFeedsToolErrorsBackToModel(t *testing.T) {
	provider := &fakeProvider{
		responses: []CompletionResponse{
			{
				Message: Message{
					Role: RoleAssistant,
					ToolCalls: []ToolCall{
						{ID: "call_1", Name: "missing"},
						{ID: "call_2", Name: "fail"},
					},
				},
			},
			{
				Message: Message{Role: RoleAssistant, Content: "recovered"},
			},
		},
	}

	failing := &fakeTool{
		name:       "fail",
		permission: PermissionRead,
		runFn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return "", errors.New("boom")
		},
	}

	reg, err := NewRegistry(failing)
	if err != nil {
		t.Fatalf("NewRegistry error: %v", err)
	}

	agent, err := NewAgent(AgentConfig{Provider: provider, Registry: reg})
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	res, err := agent.Run(context.Background(), "try")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if res.Reply != "recovered" {
		t.Errorf("got reply %q, want %q", res.Reply, "recovered")
	}

	msgs := provider.requests[1].Messages
	if !strings.Contains(msgs[2].Content, `unknown tool "missing"`) {
		t.Errorf("unexpected unknown tool message: %q", msgs[2].Content)
	}
	if !strings.Contains(msgs[3].Content, "error: boom") {
		t.Errorf("unexpected failing tool message: %q", msgs[3].Content)
	}
}

func TestAgentEnforcesMaxTurns(t *testing.T) {
	loopingResp := CompletionResponse{
		Message: Message{
			Role:      RoleAssistant,
			ToolCalls: []ToolCall{{ID: "call_1", Name: "noop"}},
		},
	}
	provider := &fakeProvider{
		responses: []CompletionResponse{loopingResp, loopingResp},
	}

	noop := &fakeTool{
		name:       "noop",
		permission: PermissionRead,
		runFn:      func(_ context.Context, _ json.RawMessage) (string, error) { return "ok", nil },
	}
	reg, err := NewRegistry(noop)
	if err != nil {
		t.Fatalf("NewRegistry error: %v", err)
	}

	agent, err := NewAgent(AgentConfig{Provider: provider, Registry: reg, MaxTurns: 2})
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	res, err := agent.Run(context.Background(), "loop")
	if !errors.Is(err, ErrMaxTurns) {
		t.Fatalf("got err %v, want ErrMaxTurns", err)
	}
	if res.Turns != 2 {
		t.Errorf("got %d turns, want 2", res.Turns)
	}
}

func TestAgentPermissionGates(t *testing.T) {
	tests := []struct {
		name        string
		permission  PermissionLevel
		approver    Approver
		wantRan     bool
		wantToolMsg string
		wantErr     error
	}{
		{
			name:        "read tool bypasses nil approver",
			permission:  PermissionRead,
			approver:    nil,
			wantRan:     true,
			wantToolMsg: "executed",
		},
		{
			name:        "write tool denied when approver is nil",
			permission:  PermissionWrite,
			approver:    nil,
			wantRan:     false,
			wantToolMsg: `error: permission denied for tool "act"`,
		},
		{
			name:       "exec tool denied by approver",
			permission: PermissionExec,
			approver: ApproverFunc(func(_ context.Context, _ ApprovalRequest) (bool, error) {
				return false, nil
			}),
			wantRan:     false,
			wantToolMsg: `error: permission denied for tool "act"`,
		},
		{
			name:       "write tool allowed by approver",
			permission: PermissionWrite,
			approver: ApproverFunc(func(_ context.Context, req ApprovalRequest) (bool, error) {
				return req.ToolName == "act" && req.Permission == PermissionWrite, nil
			}),
			wantRan:     true,
			wantToolMsg: "executed",
		},
		{
			name:       "approver error aborts loop",
			permission: PermissionExec,
			approver: ApproverFunc(func(_ context.Context, _ ApprovalRequest) (bool, error) {
				return false, context.Canceled
			}),
			wantRan: false,
			wantErr: context.Canceled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ran := false
			tool := &fakeTool{
				name:       "act",
				permission: tc.permission,
				runFn: func(_ context.Context, _ json.RawMessage) (string, error) {
					ran = true
					return "executed", nil
				},
			}
			provider := &fakeProvider{
				responses: []CompletionResponse{
					{
						Message: Message{
							Role:      RoleAssistant,
							ToolCalls: []ToolCall{{ID: "call_1", Name: "act", Arguments: json.RawMessage(`{}`)}},
						},
					},
					{
						Message: Message{Role: RoleAssistant, Content: "done"},
					},
				},
			}

			reg, err := NewRegistry(tool)
			if err != nil {
				t.Fatalf("NewRegistry error: %v", err)
			}
			agent, err := NewAgent(AgentConfig{
				Provider: provider,
				Registry: reg,
				Approver: tc.approver,
			})
			if err != nil {
				t.Fatalf("NewAgent error: %v", err)
			}

			_, err = agent.Run(context.Background(), "go")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got err %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run error: %v", err)
			}
			if ran != tc.wantRan {
				t.Errorf("tool ran=%v, want %v", ran, tc.wantRan)
			}
			gotToolMsg := provider.requests[1].Messages[2].Content
			if gotToolMsg != tc.wantToolMsg {
				t.Errorf("got tool msg %q, want %q", gotToolMsg, tc.wantToolMsg)
			}
		})
	}
}

func TestAgentTruncationCostTracingAndCompaction(t *testing.T) {
	provider := &fakeProvider{
		responses: []CompletionResponse{
			{
				FinishReason: "tool_calls",
				Message: Message{
					Role:      RoleAssistant,
					ToolCalls: []ToolCall{{ID: "c1", Name: "big"}},
				},
				Usage: Usage{InputTokens: 100_000, OutputTokens: 50_000, TotalTokens: 150_000},
			},
			{
				FinishReason: "tool_calls",
				Message: Message{
					Role:      RoleAssistant,
					ToolCalls: []ToolCall{{ID: "c2", Name: "big"}},
				},
				Usage: Usage{InputTokens: 100_000, OutputTokens: 50_000, TotalTokens: 150_000},
			},
			{
				FinishReason: "tool_calls",
				Message: Message{
					Role:      RoleAssistant,
					ToolCalls: []ToolCall{{ID: "c3", Name: "big"}},
				},
				Usage: Usage{InputTokens: 100_000, OutputTokens: 50_000, TotalTokens: 150_000},
			},
			{
				FinishReason: "stop",
				Message:      Message{Role: RoleAssistant, Content: "finished"},
				Usage:        Usage{InputTokens: 100_000, OutputTokens: 50_000, TotalTokens: 150_000},
			},
		},
	}

	bigTool := &fakeTool{
		name:       "big",
		permission: PermissionRead,
		runFn: func(_ context.Context, _ json.RawMessage) (string, error) {
			return strings.Repeat("z", 200), nil
		},
	}
	reg, err := NewRegistry(bigTool)
	if err != nil {
		t.Fatalf("NewRegistry error: %v", err)
	}

	var traceBuf bytes.Buffer
	agent, err := NewAgent(AgentConfig{
		Provider:           provider,
		Registry:           reg,
		Tracer:             NewJSONLTracer(&traceBuf),
		Pricing:            Pricing{InputPerMillion: 1.0, OutputPerMillion: 2.0},
		MaxToolOutputBytes: 40,
		MaxContextChars:    120,
	})
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	res, err := agent.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if !strings.Contains(provider.requests[1].Messages[2].Content, "truncated: showing 40 of 200 bytes") {
		t.Errorf("expected truncated tool output, got %q", provider.requests[1].Messages[2].Content)
	}
	if !strings.HasPrefix(provider.requests[3].Messages[2].Content, compactedPrefix) {
		t.Errorf("expected older tool output to be compacted, got %q", provider.requests[3].Messages[2].Content)
	}

	wantCost := 0.80
	if math.Abs(res.CostUSD-wantCost) > 1e-9 {
		t.Errorf("got cost %f, want %f", res.CostUSD, wantCost)
	}

	traceText := traceBuf.String()
	for _, wantType := range []string{`"type":"turn"`, `"type":"tool_call"`, `"type":"compaction"`, `"type":"finish"`} {
		if !strings.Contains(traceText, wantType) {
			t.Errorf("trace missing %s:\n%s", wantType, traceText)
		}
	}
}

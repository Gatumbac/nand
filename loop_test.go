package nand

import (
	"context"
	"encoding/json"
	"errors"
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

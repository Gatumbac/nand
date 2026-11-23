package nand

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const defaultMaxTurns = 10

var ErrMaxTurns = errors.New("agent reached maximum turn limit")

type AgentConfig struct {
	Provider           Provider
	Registry           *Registry
	Approver           Approver
	Tracer             Tracer
	Pricing            Pricing
	System             string
	Model              string
	MaxTurns           int
	MaxToolOutputBytes int
	MaxContextChars    int
}

type RunResult struct {
	Reply    string
	Messages []Message
	Turns    int
	Usage    Usage
	CostUSD  float64
}

type Agent struct {
	provider           Provider
	registry           *Registry
	approver           Approver
	tracer             Tracer
	pricing            Pricing
	system             string
	model              string
	maxTurns           int
	maxToolOutputBytes int
	maxContextChars    int
}

func NewAgent(cfg AgentConfig) (*Agent, error) {
	if cfg.Provider == nil {
		return nil, errors.New("agent: provider is required")
	}

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	maxOutput := cfg.MaxToolOutputBytes
	if maxOutput <= 0 {
		maxOutput = defaultMaxToolOutputBytes
	}

	return &Agent{
		provider:           cfg.Provider,
		registry:           cfg.Registry,
		approver:           cfg.Approver,
		tracer:             cfg.Tracer,
		pricing:            cfg.Pricing,
		system:             cfg.System,
		model:              cfg.Model,
		maxTurns:           maxTurns,
		maxToolOutputBytes: maxOutput,
		maxContextChars:    cfg.MaxContextChars,
	}, nil
}

func (a *Agent) Run(ctx context.Context, prompt string) (RunResult, error) {
	result := RunResult{
		Messages: []Message{{Role: RoleUser, Content: prompt}},
	}

	for turn := 1; turn <= a.maxTurns; turn++ {
		result.Messages = a.maybeCompact(turn, result.Messages)

		started := time.Now()
		resp, err := a.step(ctx, result.Messages)
		if err != nil {
			return result, err
		}

		turnCost := EstimateCost(resp.Usage, a.pricing)
		result.Turns = turn
		result.Usage = addUsage(result.Usage, resp.Usage)
		result.CostUSD += turnCost
		result.Messages = append(result.Messages, resp.Message)

		usageCopy := resp.Usage
		a.emit(TraceEvent{
			Type:         "turn",
			Turn:         turn,
			DurationMS:   time.Since(started).Milliseconds(),
			Usage:        &usageCopy,
			CostUSD:      turnCost,
			FinishReason: resp.FinishReason,
		})

		if len(resp.Message.ToolCalls) == 0 {
			result.Reply = resp.Message.Content
			totalUsage := result.Usage
			a.emit(TraceEvent{
				Type:         "finish",
				Turn:         turn,
				Usage:        &totalUsage,
				CostUSD:      result.CostUSD,
				FinishReason: resp.FinishReason,
			})
			return result, nil
		}

		toolMessages, err := a.executeToolCalls(ctx, turn, resp.Message.ToolCalls)
		if err != nil {
			return result, err
		}
		result.Messages = append(result.Messages, toolMessages...)
	}

	totalUsage := result.Usage
	a.emit(TraceEvent{
		Type:         "finish",
		Turn:         result.Turns,
		Usage:        &totalUsage,
		CostUSD:      result.CostUSD,
		FinishReason: "max_turns",
	})
	return result, ErrMaxTurns
}

func (a *Agent) maybeCompact(turn int, messages []Message) []Message {
	compacted, count := compactMessages(messages, a.maxContextChars, defaultKeepRecentMessages)
	if count > 0 {
		a.emit(TraceEvent{
			Type:   "compaction",
			Turn:   turn,
			Detail: fmt.Sprintf("compacted %d tool messages", count),
		})
	}
	return compacted
}

func (a *Agent) step(ctx context.Context, messages []Message) (CompletionResponse, error) {
	return a.provider.Complete(ctx, CompletionRequest{
		Model:    a.model,
		System:   a.system,
		Messages: messages,
		Tools:    a.registry.Definitions(),
	})
}

func (a *Agent) executeToolCalls(ctx context.Context, turn int, calls []ToolCall) ([]Message, error) {
	results := make([]Message, 0, len(calls))
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		output, err := a.executeOne(ctx, turn, call)
		if err != nil {
			return nil, err
		}
		results = append(results, Message{
			Role:       RoleTool,
			ToolCallID: call.ID,
			Content:    output,
		})
	}
	return results, nil
}

func (a *Agent) executeOne(ctx context.Context, turn int, call ToolCall) (string, error) {
	started := time.Now()
	tool, ok := a.registry.Get(call.Name)
	if !ok {
		a.emit(TraceEvent{
			Type:     "tool_call",
			Turn:     turn,
			ToolName: call.Name,
			Detail:   "unknown tool",
		})
		return fmt.Sprintf("error: unknown tool %q", call.Name), nil
	}

	allowed, err := a.checkPermission(ctx, tool, call.Arguments)
	if err != nil {
		return "", err
	}
	if !allowed {
		a.emit(TraceEvent{
			Type:       "tool_call",
			Turn:       turn,
			ToolName:   tool.Name(),
			Permission: tool.Permission(),
			Allowed:    &allowed,
			DurationMS: time.Since(started).Milliseconds(),
			Detail:     "permission denied",
		})
		return fmt.Sprintf("error: permission denied for tool %q", call.Name), nil
	}

	output, err := tool.Run(ctx, call.Arguments)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		a.emit(TraceEvent{
			Type:       "tool_call",
			Turn:       turn,
			ToolName:   tool.Name(),
			Permission: tool.Permission(),
			Allowed:    &allowed,
			DurationMS: time.Since(started).Milliseconds(),
			Detail:     err.Error(),
		})
		return fmt.Sprintf("error: %s", err.Error()), nil
	}

	output, truncated := truncateOutput(output, a.maxToolOutputBytes)
	a.emit(TraceEvent{
		Type:       "tool_call",
		Turn:       turn,
		ToolName:   tool.Name(),
		Permission: tool.Permission(),
		Allowed:    &allowed,
		DurationMS: time.Since(started).Milliseconds(),
		Truncated:  truncated,
	})
	return output, nil
}

func (a *Agent) checkPermission(ctx context.Context, tool Tool, args json.RawMessage) (bool, error) {
	if tool.Permission() == PermissionRead {
		return true, nil
	}
	if a.approver == nil {
		return false, nil
	}
	return a.approver.Approve(ctx, ApprovalRequest{
		ToolName:   tool.Name(),
		Permission: tool.Permission(),
		Arguments:  args,
	})
}

func (a *Agent) emit(event TraceEvent) {
	if a.tracer == nil {
		return
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	_ = a.tracer.WriteEvent(event)
}

func addUsage(total, delta Usage) Usage {
	return Usage{
		InputTokens:  total.InputTokens + delta.InputTokens,
		OutputTokens: total.OutputTokens + delta.OutputTokens,
		TotalTokens:  total.TotalTokens + delta.TotalTokens,
	}
}

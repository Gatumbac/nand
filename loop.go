package nand

import (
	"context"
	"errors"
	"fmt"
)

const defaultMaxTurns = 10

var ErrMaxTurns = errors.New("agent reached maximum turn limit")

type AgentConfig struct {
	Provider Provider
	Registry *Registry
	System   string
	Model    string
	MaxTurns int
}

type RunResult struct {
	Reply    string
	Messages []Message
	Turns    int
	Usage    Usage
}

type Agent struct {
	provider Provider
	registry *Registry
	system   string
	model    string
	maxTurns int
}

func NewAgent(cfg AgentConfig) (*Agent, error) {
	if cfg.Provider == nil {
		return nil, errors.New("agent: provider is required")
	}

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxTurns
	}

	return &Agent{
		provider: cfg.Provider,
		registry: cfg.Registry,
		system:   cfg.System,
		model:    cfg.Model,
		maxTurns: maxTurns,
	}, nil
}

func (a *Agent) Run(ctx context.Context, prompt string) (RunResult, error) {
	result := RunResult{
		Messages: []Message{{Role: RoleUser, Content: prompt}},
	}

	for turn := 1; turn <= a.maxTurns; turn++ {
		resp, err := a.step(ctx, result.Messages)
		if err != nil {
			return result, err
		}

		result.Turns = turn
		result.Usage = addUsage(result.Usage, resp.Usage)
		result.Messages = append(result.Messages, resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			result.Reply = resp.Message.Content
			return result, nil
		}

		toolMessages, err := a.executeToolCalls(ctx, resp.Message.ToolCalls)
		if err != nil {
			return result, err
		}
		result.Messages = append(result.Messages, toolMessages...)
	}

	return result, ErrMaxTurns
}

func (a *Agent) step(ctx context.Context, messages []Message) (CompletionResponse, error) {
	return a.provider.Complete(ctx, CompletionRequest{
		Model:    a.model,
		System:   a.system,
		Messages: messages,
		Tools:    a.registry.Definitions(),
	})
}

func (a *Agent) executeToolCalls(ctx context.Context, calls []ToolCall) ([]Message, error) {
	results := make([]Message, 0, len(calls))
	for _, call := range calls {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		output, err := a.executeOne(ctx, call)
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

func (a *Agent) executeOne(ctx context.Context, call ToolCall) (string, error) {
	tool, ok := a.registry.Get(call.Name)
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", call.Name), nil
	}

	output, err := tool.Run(ctx, call.Arguments)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return fmt.Sprintf("error: %s", err.Error()), nil
	}
	return output, nil
}

func addUsage(total, delta Usage) Usage {
	return Usage{
		InputTokens:  total.InputTokens + delta.InputTokens,
		OutputTokens: total.OutputTokens + delta.OutputTokens,
		TotalTokens:  total.TotalTokens + delta.TotalTokens,
	}
}

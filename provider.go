package nand

import "context"

type CompletionRequest struct {
	Model    string
	System   string
	Messages []Message
	Tools    []ToolDefinition
}

type CompletionResponse struct {
	Message      Message
	FinishReason string
	Usage        Usage
}

type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
